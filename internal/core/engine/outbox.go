package engine

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/rs/zerolog/log"
)

// WebhookPayload is the JSON body delivered to webhook URLs.
type WebhookPayload struct {
	Event     string `json:"event"`
	RunID     string `json:"runId"`
	ProjectID string `json:"projectId"`
	Status    string `json:"status"`
	Branch    string `json:"branch,omitempty"`
	CommitSHA string `json:"commitSha,omitempty"`
	Duration  int    `json:"durationMs,omitempty"`
	Timestamp string `json:"timestamp"`
}

// processOutbox claims a batch of pending outbox events and delivers them.
// Called once per tick from the engine loop.
func processOutbox(ctx context.Context, pool db.Pool) {
	q := db.New(pool)
	events, err := q.ClaimOutboxBatch(ctx, 10)
	if err != nil {
		log.Error().Err(err).Msg("outbox: claim batch failed")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, evt := range events {
		switch evt.EventType {
		case "webhook":
			deliverWebhook(ctx, q, client, evt)
		default:
			log.Warn().Str("type", evt.EventType).Msg("outbox: unknown event type, resolving")
			if err := q.ResolveOutboxEvent(ctx, evt.ID); err != nil {
				log.Error().Err(err).Str("id", evt.ID).Msg("outbox: failed to resolve unknown event")
			}
		}
	}
}

// webhookOutboxPayload wraps the delivery target and body.
type webhookOutboxPayload struct {
	URL     string         `json:"url"`
	Secret  string         `json:"secret"`
	Webhook WebhookPayload `json:"webhook"`
}

func deliverWebhook(ctx context.Context, q *db.Queries, client *http.Client, evt db.ClaimOutboxBatchRow) {
	var payload webhookOutboxPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		log.Error().Err(err).Str("id", evt.ID).Msg("outbox: invalid webhook payload")
		_ = q.ResolveOutboxEvent(ctx, evt.ID) // bad data, don't retry
		return
	}

	body, _ := json.Marshal(payload.Webhook)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, payload.URL, bytes.NewReader(body))
	if err != nil {
		failEvent(ctx, q, evt.ID, fmt.Sprintf("bad URL: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Flint-Webhook/1.0")

	// HMAC signature for verification (same pattern as GitHub webhooks).
	if payload.Secret != "" {
		mac := hmac.New(sha256.New, []byte(payload.Secret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Flint-Signature", "sha256="+sig)
	}

	resp, err := client.Do(req)
	if err != nil {
		failEvent(ctx, q, evt.ID, fmt.Sprintf("delivery failed: %v", err))
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := q.ResolveOutboxEvent(ctx, evt.ID); err != nil {
			log.Error().Err(err).Str("id", evt.ID).Msg("outbox: failed to resolve event")
		}
		observe.OutboxEventsProcessed.Add(ctx, 1)
		log.Debug().Str("url", payload.URL).Int("status", resp.StatusCode).Msg("outbox: webhook delivered")
	} else {
		observe.OutboxEventsFailed.Add(ctx, 1)
		failEvent(ctx, q, evt.ID, fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
}

func failEvent(ctx context.Context, q *db.Queries, id, errMsg string) {
	if err := q.FailOutboxEvent(ctx, db.FailOutboxEventParams{
		ID:        id,
		LastError: &errMsg,
	}); err != nil {
		log.Error().Err(err).Str("id", id).Msg("outbox: failed to record event failure")
	}
}

// enqueueWebhooksInTx looks up all active webhooks for a run and inserts
// outbox events for each. Runs inside the same transaction as finishWorkflow
// so that if the transaction rolls back, no spurious webhooks are sent.
func enqueueWebhooksInTx(ctx context.Context, q *db.Queries, runID, status string) {
	eventName := "run.completed"
	if status == "failed" {
		eventName = "run.failed"
	} else if status == "cancelled" {
		eventName = "run.cancelled"
	}

	hooks, err := q.GetActiveWebhooksForEvent(ctx, db.GetActiveWebhooksForEventParams{
		ID:      runID,
		Column2: eventName,
	})
	if err != nil {
		// No webhooks configured — common case, not an error.
		return
	}
	if len(hooks) == 0 {
		return
	}

	// Build the webhook payload from the run.
	run, err := q.GetRun(ctx, runID)
	if err != nil {
		log.Warn().Err(err).Str("runID", runID).Msg("outbox: failed to fetch run for webhook")
		return
	}

	basePayload := WebhookPayload{
		Event:     eventName,
		RunID:     runID,
		Status:    run.Status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if run.ProjectID != nil {
		basePayload.ProjectID = *run.ProjectID
	}
	if run.TriggerRef != nil {
		basePayload.Branch = *run.TriggerRef
	}
	if run.CommitSha != nil {
		basePayload.CommitSHA = *run.CommitSha
	}
	if run.DurationMs.Valid {
		basePayload.Duration = int(run.DurationMs.Int32)
	}

	for _, hook := range hooks {
		outboxPayload := webhookOutboxPayload{
			URL:     hook.Url,
			Secret:  hook.Secret,
			Webhook: basePayload,
		}
		payloadJSON, _ := json.Marshal(outboxPayload)
		idempotencyKey := fmt.Sprintf("webhook:%s:%s:%s", hook.ID, runID, eventName)

		if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{
			EventType:      "webhook",
			Payload:        payloadJSON,
			IdempotencyKey: idempotencyKey,
			MaxAttempts:    5,
		}); err != nil {
			log.Warn().Err(err).Str("webhookID", hook.ID).Str("runID", runID).
				Msg("outbox: failed to enqueue webhook event")
		}
	}

	log.Info().Str("runID", runID).Int("webhooks", len(hooks)).Msg("outbox: enqueued webhook events")
}
