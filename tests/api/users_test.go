package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gotd/contrib/storage"
	"github.com/tgdrive/teldrive/internal/api"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/services"
)

func TestUsersRoutes_BasicEndpoints(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7206, "user7206")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(900050), ChannelName: api.NewOptString("primary")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}
	if _, err := client.UsersStats(ctx); err != nil {
		t.Fatalf("UsersStats failed: %v", err)
	}
	if _, err := client.UsersListSessions(ctx); err != nil {
		t.Fatalf("UsersListSessions failed: %v", err)
	}
	if _, err := client.UsersListChannels(ctx); err != nil {
		t.Fatalf("UsersListChannels failed: %v", err)
	}
	if _, err := client.UsersProfileImage(ctx); err != nil {
		t.Fatalf("UsersProfileImage failed: %v", err)
	}
	if err := client.UsersRemoveBots(ctx); err != nil {
		t.Fatalf("UsersRemoveBots failed: %v", err)
	}
}

func TestUsersRoutes_ValidationAndRollback(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7002, "user7002")

	t.Run("UsersDeleteChannel invalid id => 400", func(t *testing.T) {
		err := client.UsersDeleteChannel(ctx, api.UsersDeleteChannelParams{ID: "not-number"})
		sc := statusCode(err)
		if sc != 400 {
			t.Fatalf("expected 400, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 400 {
			t.Fatalf("expected body.code=400, got %d", eb.Code)
		}
	})

	t.Run("UsersUpdateChannel missing channelId => 400", func(t *testing.T) {
		err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{})
		sc := statusCode(err)
		if sc != 400 {
			t.Fatalf("expected 400, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 400 {
			t.Fatalf("expected body.code=400, got %d", eb.Code)
		}
	})

	t.Run("UsersRemoveSession foreign session => 404", func(t *testing.T) {
		_, _, foreignHash := loginWithClient(t, s, 7003, "user7003")
		err := client.UsersRemoveSession(ctx, api.UsersRemoveSessionParams{ID: api.UUID(uuid.MustParse(foreignHash))})
		sc := statusCode(err)
		if sc != 404 {
			t.Fatalf("expected 404, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 404 {
			t.Fatalf("expected body.code=404, got %d", eb.Code)
		}
	})
}

func TestUsersListChannels(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 8401, "user8401")

	// With no peer KV data, UsersListChannels returns an empty list.
	channels, err := client.UsersListChannels(ctx)
	if err != nil {
		t.Fatalf("UsersListChannels failed: %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("expected empty channel list, got %d items", len(channels))
	}
}

func TestUsersProfileImageEmpty(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 8402, "user8402")

	// profilePhotoFn defaults (nil, nil, false, nil) — no photo, no error.
	_, err := client.UsersProfileImage(ctx)
	if err != nil {
		t.Fatalf("UsersProfileImage failed: %v", err)
	}
}

func TestUsersRoutes_TelegramFailureScenarios(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, sessionHash := loginWithClient(t, s, 7208, "user7208")

	t.Run("UsersDeleteChannel telegram delete failure => 500", func(t *testing.T) {
		selected := true
		if err := s.repos.Channels.Create(ctx, &jetmodel.Channels{UserID: 7208, ChannelID: 920001, ChannelName: "to-delete", Selected: &selected}); err != nil {
			t.Fatalf("seed channel: %v", err)
		}
		s.tgMock.deleteChannelFn = func(context.Context, services.TelegramClient, int64) (storage.PeerKey, error) {
			return storage.PeerKey{}, errors.New("delete channel failed")
		}
		err := client.UsersDeleteChannel(ctx, api.UsersDeleteChannelParams{ID: "920001"})
		if statusCode(err) != 500 {
			t.Fatalf("expected 500, got %d err=%v", statusCode(err), err)
		}
		s.tgMock.deleteChannelFn = nil
	})

	t.Run("UsersSyncChannels auth client failure => 500", func(t *testing.T) {
		s.tgMock.authClientFn = func(context.Context, string, int) (services.TelegramClient, error) {
			return nil, errors.New("auth client failed")
		}
		err := client.UsersSyncChannels(ctx)
		if statusCode(err) != 500 {
			t.Fatalf("expected 500, got %d err=%v", statusCode(err), err)
		}
		s.tgMock.authClientFn = nil
	})

	t.Run("UsersListSessions authorizations failure => 500", func(t *testing.T) {
		s.tgMock.listAuthsFn = func(context.Context, services.TelegramClient) ([]services.TelegramAuthorization, error) {
			return nil, errors.New("list auths failed")
		}
		_, err := client.UsersListSessions(ctx)
		if statusCode(err) != 500 {
			t.Fatalf("expected 500, got %d err=%v", statusCode(err), err)
		}
		s.tgMock.listAuthsFn = nil
	})

	t.Run("UsersProfileImage telegram failure => 500", func(t *testing.T) {
		s.tgMock.profilePhotoFn = func(context.Context, services.TelegramClient) ([]byte, int64, bool, error) {
			return nil, 0, false, errors.New("profile failed")
		}
		_, err := client.UsersProfileImage(ctx)
		if statusCode(err) != 500 {
			t.Fatalf("expected 500, got %d err=%v", statusCode(err), err)
		}
		s.tgMock.profilePhotoFn = nil
	})

	t.Run("UsersRemoveSession logout failure still revokes session", func(t *testing.T) {
		s.tgMock.logoutFn = func(context.Context, services.TelegramClient) error {
			return errors.New("logout failed")
		}
		err := client.UsersRemoveSession(ctx, api.UsersRemoveSessionParams{ID: api.UUID(uuid.MustParse(sessionHash))})
		if statusCode(err) != 200 {
			t.Fatalf("expected 200, got %d err=%v", statusCode(err), err)
		}
		if _, getErr := s.repos.Sessions.GetByID(ctx, uuid.MustParse(sessionHash)); getErr == nil {
			t.Fatalf("expected session to be revoked despite telegram logout failure")
		}
		s.tgMock.logoutFn = nil
	})
}
