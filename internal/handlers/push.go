package handlers

import (
	"encoding/json"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// PushSubscribeRequest mirrors the browser's PushSubscription.toJSON() shape.
type PushSubscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// PushUnsubscribeRequest identifies the subscription to remove.
type PushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// pushEnabled reports whether Web Push is fully configured.
func (a *App) pushEnabled() bool {
	p := a.Config.Push
	return p.Enabled && p.VAPIDPublicKey != "" && p.VAPIDPrivateKey != ""
}

// GetVAPIDKey returns the server's VAPID public key so the frontend can
// subscribe. Enabled=false tells the client to skip push entirely.
func (a *App) GetVAPIDKey(r *fastglue.Request) error {
	_, _, err := a.requireAuth(r, models.ResourceChat, models.ActionRead)
	if err != nil {
		return nil
	}

	return r.SendEnvelope(map[string]any{
		"enabled":    a.pushEnabled(),
		"public_key": a.Config.Push.VAPIDPublicKey,
	})
}

// SubscribePush stores (or reassigns) a Web Push subscription for the
// authenticated user. Upserts by endpoint: the same browser profile can switch
// users, in which case the subscription must follow the new login.
func (a *App) SubscribePush(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceChat, models.ActionRead)
	if err != nil {
		return nil
	}

	var req PushSubscribeRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}
	if req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "endpoint and keys are required", nil, "")
	}

	userAgent := string(r.RequestCtx.Request.Header.UserAgent())
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}

	var sub models.PushSubscription
	res := a.DB.Unscoped().Where("endpoint = ?", req.Endpoint).First(&sub)
	if res.Error == nil {
		sub.OrganizationID = orgID
		sub.UserID = userID
		sub.P256dh = req.Keys.P256dh
		sub.Auth = req.Keys.Auth
		sub.UserAgent = userAgent
		sub.DeletedAt.Valid = false
		if err := a.DB.Unscoped().Save(&sub).Error; err != nil {
			a.Log.Error("Failed to update push subscription", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to save subscription", nil, "")
		}
	} else {
		sub = models.PushSubscription{
			OrganizationID: orgID,
			UserID:         userID,
			Endpoint:       req.Endpoint,
			P256dh:         req.Keys.P256dh,
			Auth:           req.Keys.Auth,
			UserAgent:      userAgent,
		}
		if err := a.DB.Create(&sub).Error; err != nil {
			a.Log.Error("Failed to create push subscription", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to save subscription", nil, "")
		}
	}

	return r.SendEnvelope(map[string]string{"message": "Subscribed"})
}

// UnsubscribePush removes the authenticated user's subscription for the given
// endpoint (e.g. on logout).
func (a *App) UnsubscribePush(r *fastglue.Request) error {
	_, userID, err := a.requireAuth(r, models.ResourceChat, models.ActionRead)
	if err != nil {
		return nil
	}

	var req PushUnsubscribeRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}
	if req.Endpoint == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "endpoint is required", nil, "")
	}

	if err := a.DB.Unscoped().
		Where("endpoint = ? AND user_id = ?", req.Endpoint, userID).
		Delete(&models.PushSubscription{}).Error; err != nil {
		a.Log.Error("Failed to delete push subscription", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to delete subscription", nil, "")
	}

	return r.SendEnvelope(map[string]string{"message": "Unsubscribed"})
}

// sendNewMessagePush delivers a Web Push notification for an incoming message
// to the same recipients the in-page WebSocket alert targets (websocket.ts):
// the assigned agent, or every org user when the message sits in the queue.
// Users with new_message_alerts=false are skipped. The service worker decides
// whether to actually show it (it suppresses when a focused window exists).
func (a *App) sendNewMessagePush(orgID uuid.UUID, contact *models.Contact, msg *models.Message, profileName string) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()

		query := a.DB.Model(&models.PushSubscription{}).
			Joins("JOIN users ON users.id = push_subscriptions.user_id AND users.deleted_at IS NULL").
			Where("push_subscriptions.organization_id = ?", orgID).
			Where("(users.settings->>'new_message_alerts') IS DISTINCT FROM 'false'")
		if contact.AssignedUserID != nil {
			query = query.Where("push_subscriptions.user_id = ?", *contact.AssignedUserID)
		}

		var subs []models.PushSubscription
		if err := query.Find(&subs).Error; err != nil {
			a.Log.Error("Failed to load push subscriptions", "error", err)
			return
		}
		if len(subs) == 0 {
			return
		}

		title := profileName
		if title == "" {
			title = "New message"
		}
		body := msg.Content
		if body == "" {
			body = "New message"
		}
		if runes := []rune(body); len(runes) > 50 {
			body = string(runes[:50]) + "..."
		}

		payload, err := json.Marshal(map[string]string{
			"type":       "new_message",
			"title":      title,
			"body":       body,
			"contact_id": contact.ID.String(),
			"tag":        "chat-" + contact.ID.String(),
			"url":        "/chat/" + contact.ID.String(),
		})
		if err != nil {
			a.Log.Error("Failed to marshal push payload", "error", err)
			return
		}

		opts := &webpush.Options{
			Subscriber:      a.Config.Push.Subscriber,
			VAPIDPublicKey:  a.Config.Push.VAPIDPublicKey,
			VAPIDPrivateKey: a.Config.Push.VAPIDPrivateKey,
			TTL:             300,
			Urgency:         webpush.UrgencyHigh,
		}

		for _, sub := range subs {
			s := &webpush.Subscription{
				Endpoint: sub.Endpoint,
				Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
			}
			resp, err := webpush.SendNotification(payload, s, opts)
			if err != nil {
				a.Log.Warn("Web push delivery failed", "endpoint", sub.Endpoint, "error", err)
				continue
			}
			// 404/410 means the browser revoked the subscription — drop it.
			if resp.StatusCode == fasthttp.StatusNotFound || resp.StatusCode == fasthttp.StatusGone {
				a.DB.Unscoped().Delete(&models.PushSubscription{}, "id = ?", sub.ID)
			}
			_ = resp.Body.Close()
		}
	}()
}
