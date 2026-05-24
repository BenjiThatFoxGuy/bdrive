package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/tgdrive/teldrive/internal/api"
)

func TestUsersApiKeys_ExpiryAndNoExpiry(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7210, "user7210")

	t.Run("create key without expiry and authenticate", func(t *testing.T) {
		created, err := client.UsersCreateApiKey(ctx, &api.UserApiKeyCreate{Name: "permanent-key"})
		if err != nil {
			t.Fatalf("UsersCreateApiKey failed: %v", err)
		}
		if created.Key == "" {
			t.Fatalf("expected plaintext key in create response")
		}
		if created.ExpiresAt.Set {
			t.Fatalf("expected no expiry for permanent key")
		}

		apiKeyClient := s.newClientWithToken(created.Key)
		if _, err := apiKeyClient.UsersStats(ctx); err != nil {
			t.Fatalf("UsersStats with API key failed: %v", err)
		}

		keys, err := client.UsersListApiKeys(ctx)
		if err != nil {
			t.Fatalf("UsersListApiKeys failed: %v", err)
		}
		found := false
		for _, key := range keys {
			if key.ID == created.ID {
				found = true
				if key.ExpiresAt.Set {
					t.Fatalf("expected listed key to have no expiry")
				}
				break
			}
		}
		if !found {
			t.Fatalf("created key not found in list")
		}
	})

	t.Run("create key with past expiry is rejected", func(t *testing.T) {
		_, err := client.UsersCreateApiKey(ctx, &api.UserApiKeyCreate{
			Name:      "invalid-expiry",
			ExpiresAt: api.NewOptDateTime(time.Now().UTC().Add(-time.Minute)),
		})
		sc := statusCode(err)
		if sc != 400 {
			t.Fatalf("expected 400 for past expiry, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 400 {
			t.Fatalf("expected body.code=400, got %d", eb.Code)
		}
	})

	t.Run("revoked key is rejected", func(t *testing.T) {
		created, err := client.UsersCreateApiKey(ctx, &api.UserApiKeyCreate{Name: "revoke-key"})
		if err != nil {
			t.Fatalf("UsersCreateApiKey failed: %v", err)
		}

		apiKeyClient := s.newClientWithToken(created.Key)
		if _, err := apiKeyClient.UsersStats(ctx); err != nil {
			t.Fatalf("UsersStats with API key before revoke failed: %v", err)
		}

		if err := client.UsersRemoveApiKey(ctx, api.UsersRemoveApiKeyParams{ID: created.ID}); err != nil {
			t.Fatalf("UsersRemoveApiKey failed: %v", err)
		}

		_, err = apiKeyClient.UsersStats(ctx)
		sc := statusCode(err)
		if sc != 401 {
			t.Fatalf("expected 401 for revoked api key, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil {
			if eb.Error.Value != "unauthorized" && eb.Error.Value != "" {
				t.Logf("revoked key body error=%s message=%s", eb.Error.Value, eb.Message)
			}
		}
	})

	t.Run("api key requires at least one valid session", func(t *testing.T) {
		created, err := client.UsersCreateApiKey(ctx, &api.UserApiKeyCreate{Name: "session-required-key"})
		if err != nil {
			t.Fatalf("UsersCreateApiKey failed: %v", err)
		}

		apiKeyClient := s.newClientWithToken(created.Key)
		if _, err := apiKeyClient.UsersStats(ctx); err != nil {
			t.Fatalf("UsersStats with API key before session revoke failed: %v", err)
		}

		sessions, err := client.UsersListSessions(ctx)
		if err != nil {
			t.Fatalf("UsersListSessions failed: %v", err)
		}
		if len(sessions) == 0 {
			t.Fatalf("expected at least one session")
		}

		if err := client.UsersRemoveSession(ctx, api.UsersRemoveSessionParams{ID: sessions[0].SessionId}); err != nil {
			t.Fatalf("UsersRemoveSession failed: %v", err)
		}

		_, err = apiKeyClient.UsersStats(ctx)
		sc := statusCode(err)
		if sc != 401 {
			t.Fatalf("expected 401 for api key without any valid session, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 401 {
			t.Fatalf("expected body.code=401, got %d", eb.Code)
		}
	})

	t.Run("api key cache is invalidated on auth logout", func(t *testing.T) {
		_, logoutClient, _ := loginWithClient(t, s, 7211, "user7211")

		created, err := logoutClient.UsersCreateApiKey(ctx, &api.UserApiKeyCreate{Name: "logout-invalidation-key"})
		if err != nil {
			t.Fatalf("UsersCreateApiKey failed: %v", err)
		}

		apiKeyClient := s.newClientWithToken(created.Key)
		if _, err := apiKeyClient.UsersStats(ctx); err != nil {
			t.Fatalf("UsersStats with API key before logout failed: %v", err)
		}

		if _, err := logoutClient.AuthLogout(ctx); err != nil {
			t.Fatalf("AuthLogout failed: %v", err)
		}

		_, err = apiKeyClient.UsersStats(ctx)
		sc := statusCode(err)
		if sc != 401 {
			t.Fatalf("expected 401 for api key after logout invalidation, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 401 {
			t.Fatalf("expected body.code=401, got %d", eb.Code)
		}
	})
}
