package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/ogen-go/ogen/ogenerrors"
	"go.uber.org/zap"

	ht "github.com/ogen-go/ogen/http"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/events"
	"github.com/tgdrive/teldrive/internal/filecache"
	"github.com/tgdrive/teldrive/internal/logging"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/internal/tgstorage"
	"github.com/tgdrive/teldrive/internal/utils"
	"github.com/tgdrive/teldrive/internal/version"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"gorm.io/gorm"
)

type apiService struct {
	db             *gorm.DB
	cnf            *config.ServerCmdConfig
	cache          cache.Cacher
	botSelector    tgc.BotSelector
	events         events.EventBroadcaster
	channelManager *tgc.ChannelManager
	dedup          *dedupManager
	// zipSlots caps how many archives stream at once. nil when the limit is
	// disabled.
	zipSlots chan struct{}
	// fileCache holds decrypted file contents so a repeat request for the same
	// file does not go back to Telegram. Nil when disabled.
	fileCache *filecache.Cache
}

func (a *apiService) newMiddlewares(ctx context.Context, retries int) []telegram.Middleware {
	return tgc.NewMiddleware(&a.cnf.TG,
		tgc.WithFloodWait(),
		tgc.WithRecovery(ctx),
		tgc.WithRetry(retries),
		tgc.WithRateLimit(),
	)
}

func (a *apiService) VersionVersion(ctx context.Context) (*api.ApiVersion, error) {
	return version.GetVersionInfo(), nil
}

func (a *apiService) ConfigConfig(ctx context.Context) (*api.ServerConfig, error) {
	res := &api.ServerConfig{
		ZipDownloadEnabled: a.cnf.Files.EnableZipDownload,
		ShortlinksEnabled:  a.cnf.Shortlinks.Enabled,
	}
	if a.cnf.Shortlinks.Domain != "" {
		res.ShortlinkDomain = api.NewOptString(a.cnf.Shortlinks.Domain)
	}
	if a.cnf.Server.ResizerHost != "" {
		res.ResizerHost = api.NewOptString(a.cnf.Server.ResizerHost)
	}
	return res, nil
}

func (a *apiService) EventsGetEvents(ctx context.Context) ([]api.Event, error) {
	//Get latest events within 5 minutes
	userId := auth.GetUser(ctx)
	res := []models.Event{}
	a.db.Model(&models.Event{}).Where("created_at > ?", time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339)).
		Where("user_id = ?", userId).Order("created_at desc").Find(&res)
	return utils.Map(res, func(item models.Event) api.Event {
		return api.Event{
			ID:        item.ID,
			Type:      item.Type,
			CreatedAt: item.CreatedAt,
			Source: api.Source{
				ID:           item.Source.Data().ID,
				Type:         api.SourceType(item.Source.Data().Type),
				Name:         item.Source.Data().Name,
				ParentId:     item.Source.Data().ParentID,
				DestParentId: api.NewOptString(item.Source.Data().DestParentID),
			},
		}
	}), nil
}

func (a *apiService) EventsEventsStream(ctx context.Context, params api.EventsEventsStreamParams) (*api.EventsEventsStreamOKHeaders, error) {
	return nil, nil
}

func (e *extendedService) EventsEventsStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		http.Error(w, "missing token or authash", http.StatusUnauthorized)
		return
	}
	user, err := auth.VerifyUser(r.Context(), e.api.db, e.api.cache, e.api.cnf.JWT.Secret, cookie.Value)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	userId, _ := strconv.ParseInt(user.Subject, 10, 64)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	eventChan := e.api.events.Subscribe(userId)
	defer e.api.events.Unsubscribe(userId, eventChan)
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			src := event.Source.Data()
			if src == nil {
				continue
			}
			eventData := api.Event{
				ID:        event.ID,
				Type:      event.Type,
				CreatedAt: event.CreatedAt,
				Source: api.Source{
					ID:           src.ID,
					Type:         api.SourceType(src.Type),
					Name:         src.Name,
					ParentId:     src.ParentID,
					DestParentId: api.NewOptString(src.DestParentID),
				},
			}

			jsonData, _ := eventData.MarshalJSON()
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (a *apiService) NewError(ctx context.Context, err error) *api.ErrorStatusCode {
	var (
		code     = http.StatusInternalServerError
		message  = http.StatusText(code)
		ogenErr  ogenerrors.Error
		apiError *apiError
	)
	switch {
	case errors.Is(err, ht.ErrNotImplemented):
		code = http.StatusNotImplemented
		message = http.StatusText(code)
	case errors.As(err, &ogenErr):
		code = ogenErr.Code()
		message = ogenErr.Error()
	case errors.As(err, &apiError):
		if apiError.code == 0 {
			code = http.StatusInternalServerError
			message = http.StatusText(code)
		} else {
			code = apiError.code
			message = apiError.Error()
		}
		logger := logging.Component("API")
		logger.Error("request.failed", zap.Error(apiError.err))
	}
	return &api.ErrorStatusCode{StatusCode: code, Response: api.Error{Code: code, Message: message}}
}

func NewApiService(db *gorm.DB,
	cnf *config.ServerCmdConfig,
	cache cache.Cacher,
	botSelector tgc.BotSelector,
	events events.EventBroadcaster,
	fileCache *filecache.Cache) *apiService {

	var zipSlots chan struct{}
	if limit := cnf.Files.ZipMaxConcurrent; limit > 0 {
		zipSlots = make(chan struct{}, limit)
	}

	return &apiService{
		db:             db,
		cnf:            cnf,
		cache:          cache,
		botSelector:    botSelector,
		events:         events,
		channelManager: tgc.NewChannelManager(db, cache, &cnf.TG),
		dedup:          newDedupManager(),
		zipSlots:       zipSlots,
		fileCache:      fileCache,
	}
}

type extendedService struct {
	api *apiService
}

func NewExtendedService(api *apiService) *extendedService {
	return &extendedService{api: api}
}

type extendedMiddleware struct {
	next *api.Server
	srv  *extendedService
}

func (m *extendedMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Video thumbnail endpoint (not in OGen spec).
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/thumb/") {
		fileId := strings.TrimPrefix(r.URL.Path, "/files/thumb/")
		if fileId != "" {
			m.srv.FileThumbnail(w, r, fileId)
			return
		}
	}

	// Archive virtual folder endpoints (not in OGen spec).
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/zip/") {
		rest := strings.TrimPrefix(r.URL.Path, "/files/zip/")
		if idx := strings.Index(rest, "/"); idx > 0 {
			fileId := rest[:idx]
			action := rest[idx+1:]
			switch action {
			case "list":
				m.srv.ZipBrowse(w, r, fileId)
				return
			case "file":
				m.srv.ZipExtract(w, r, fileId)
				return
			}
		}
	}

	// CBZ (Comic Book Archive) virtual folder endpoints.
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/cbz/") {
		rest := strings.TrimPrefix(r.URL.Path, "/files/cbz/")
		if idx := strings.Index(rest, "/"); idx > 0 {
			fileId := rest[:idx]
			action := rest[idx+1:]
			switch action {
			case "list":
				m.srv.CbzBrowse(w, r, fileId)
				return
			case "file":
				m.srv.CbzExtract(w, r, fileId)
				return
			case "metadata":
				m.srv.CbzMetadata(w, r, fileId)
				return
			case "cover":
				m.srv.CbzCover(w, r, fileId)
				return
			}
		}
	}

	// Unitypackage virtual folder endpoints.
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/unitypkg/") {
		rest := strings.TrimPrefix(r.URL.Path, "/files/unitypkg/")
		if idx := strings.Index(rest, "/"); idx > 0 {
			fileId := rest[:idx]
			action := rest[idx+1:]
			switch action {
			case "list":
				m.srv.UnityPkgBrowse(w, r, fileId)
				return
			case "file":
				m.srv.UnityPkgExtract(w, r, fileId)
				return
			}
		}
	}

	route, ok := m.next.FindRoute(r.Method, r.URL.Path)
	if !ok {
		m.next.ServeHTTP(w, r)
		return
	}
	switch route.Name() {
	case api.AuthWsOperation:
		m.srv.AuthWs(w, r)
		return
	case api.FilesStreamOperation:
		args := route.Args()
		m.srv.FilesStream(w, r, args[0], 0)
		return
	case api.SharesStreamOperation:
		args := route.Args()
		m.srv.SharesStream(w, r, args[0], args[1])
		return

	case api.EventsEventsStreamOperation:
		m.srv.EventsEventsStream(w, r)
		return
	}
	m.next.ServeHTTP(w, r)
}

func NewExtendedMiddleware(next *api.Server, srv *extendedService) *extendedMiddleware {
	return &extendedMiddleware{next: next, srv: srv}
}

type apiError struct {
	err  error
	code int
}

func (a apiError) Error() string {
	return a.err.Error()
}

func (a *apiError) Unwrap() error {
	return a.err
}

var (
	_ api.Handler = (*apiService)(nil)
	_ error       = apiError{}
)

// BotTokenCleanup tests every bot token configured for the user and removes the
// ones that are definitively invalid. Tokens that are merely rate-limited or
// temporarily unreachable are kept and reported, so a transient failure never
// deletes a working bot.
func (a *apiService) BotTokenCleanup(ctx context.Context) (*BotTokenCleanupResult, error) {
	userId := auth.GetUser(ctx)
	if userId == 0 {
		return nil, errors.New("unauthenticated")
	}

	var bots []models.Bot
	if err := a.db.Where("user_id = ?", userId).Find(&bots).Error; err != nil {
		return nil, errors.Wrap(err, "list bots")
	}

	peerStorage := tgstorage.NewPeerStorage(a.db, cache.KeyPeer(userId))

	result := &BotTokenCleanupResult{
		Valid:   []BotTokenInfo{},
		Invalid: []BotTokenInfo{},
		Kept:    []BotTokenInfo{},
	}

	for _, bot := range bots {
		botID := botIDFromToken(bot.BotId, bot.Token)
		userName := knownBotUsername(ctx, peerStorage, botID)

		info, err := tgc.GetBotInfo(ctx, a.db, a.cache, &a.cnf.TG, bot.Token)
		switch {
		case err == nil:
			if userName == "" {
				userName = info.UserName
			}
			// A working token: bring its stored records up to date. GetBotInfo
			// re-authenticated against Telegram just now, so the bot's gotd
			// session is refreshed too.
			a.refreshBotRecord(ctx, userId, peerStorage, bot, info)
			result.Valid = append(result.Valid, BotTokenInfo{
				BotId:    botID,
				UserName: userName,
				token:    bot.Token,
			})
		case isDefiniteBotAuthFailure(err):
			result.Invalid = append(result.Invalid, BotTokenInfo{
				BotId:    botID,
				UserName: userName,
				Error:    err.Error(),
				token:    bot.Token,
			})
		default:
			// Rate-limited, network failure, timeout, ... keep the token.
			result.Kept = append(result.Kept, BotTokenInfo{
				BotId:    botID,
				UserName: userName,
				Error:    err.Error(),
				token:    bot.Token,
			})
		}
	}

	result.TotalScanned = len(bots)

	if len(result.Invalid) > 0 {
		if err := a.removeBots(ctx, userId, peerStorage, result.Invalid); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// refreshBotRecord brings a valid bot's stored metadata up to date.
//
// The bot id is corrected when an older row was saved as zero, and the peer is
// (re)written so a later cleanup can name the bot instead of reporting
// "unknown username". Re-authenticating the token also refreshes the bot's gotd
// session in the database-backed session storage.
func (a *apiService) refreshBotRecord(ctx context.Context, userId int64, peerStorage *tgstorage.PeerStorage, bot models.Bot, info *types.BotInfo) {
	logger := logging.Component("API")

	if info.Id != 0 && info.Id != bot.BotId {
		if err := a.db.Model(&models.Bot{}).
			Where("user_id = ? AND token = ?", userId, bot.Token).
			Update("bot_id", info.Id).Error; err != nil {
			logger.Warn("bot.bot_id_refresh_failed", zap.Error(err))
		}
	}
	if info.Id == 0 {
		return
	}

	peer := storage.Peer{
		Key:  dialogs.DialogKey{Kind: dialogs.User, ID: info.Id},
		User: &tg.User{ID: info.Id, AccessHash: info.AccessHash, Username: info.UserName, Bot: true},
	}
	if err := peerStorage.Add(ctx, peer); err != nil {
		logger.Warn("bot.peer_refresh_failed", zap.Error(err))
	}
}

// knownBotUsername returns the last username cached for a bot peer, if any.
func knownBotUsername(ctx context.Context, peerStorage *tgstorage.PeerStorage, botId int64) string {
	peer, err := peerStorage.Find(ctx, storage.PeerKey{Kind: dialogs.User, ID: botId})
	if err != nil || peer.User == nil {
		return ""
	}
	username, _ := peer.User.GetUsername()
	return username
}

// isDefiniteBotAuthFailure reports whether the error proves the token itself is
// bad (revoked, reset, deactivated) rather than the request being throttled or
// the network being unavailable.
func isDefiniteBotAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	if tgerr.Is(err,
		"AUTH_KEY_INVALID",
		"AUTH_KEY_UNREGISTERED",
		"AUTH_TOKEN_INVALID",
		"AUTH_TOKEN_INVALIDX",
		"AUTH_TOKEN_EXPIRED",
		"ACCESS_TOKEN_INVALID",
		"ACCESS_TOKEN_EXPIRED",
		"BOT_INVALID",
	) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"bot was deactivated",
		"bot was revoked",
		"bot token is invalid",
		"auth_key_invalid",
		"auth_token_invalid",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// botIDFromToken falls back to the ID encoded in the token itself when the
// database row does not have one (older rows were saved with bot_id 0). A bot
// token is "<bot id>:<secret>", so the prefix identifies the bot without
// revealing the secret.
func botIDFromToken(stored int64, token string) int64 {
	if stored != 0 {
		return stored
	}
	prefix, _, ok := strings.Cut(token, ":")
	if !ok {
		return stored
	}
	id, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return stored
	}
	return id
}

// removeBots deletes the given bots from the database and drops their cached
// peers so nothing keeps pointing at a token that no longer works.
func (a *apiService) removeBots(ctx context.Context, userId int64, peerStorage *tgstorage.PeerStorage, bots []BotTokenInfo) error {
	// Delete by token: it is the primary key and always correct, unlike bot_id,
	// which older rows may have left at zero.
	for _, bot := range bots {
		if bot.token == "" {
			continue
		}
		if err := a.db.Where("user_id = ? AND token = ?", userId, bot.token).Delete(&models.Bot{}).Error; err != nil {
			return errors.Wrap(err, "delete bot")
		}
		if err := peerStorage.Delete(ctx, storage.PeerKey{Kind: dialogs.User, ID: bot.BotId}); err != nil {
			return errors.Wrap(err, "delete bot peer")
		}
	}

	a.cache.Delete(ctx, cache.KeyUserBots(userId))
	return nil
}

// BotTokenCleanupHandler is the HTTP handler for bot token cleanup.
func (a *apiService) BotTokenCleanupHandler(w http.ResponseWriter, r *http.Request) {
	ctx, err := auth.FromRequest(r.Context(), a.db, a.cache, a.cnf.JWT.Secret, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	result, err := a.BotTokenCleanup(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		logger := logging.Component("API")
		logger.Error("response.write", zap.Error(err))
	}
}

// BotTokenInfo describes one bot found during a cleanup run. The token itself is
// never included.
type BotTokenInfo struct {
	BotId    int64  `json:"bot_id"`
	UserName string `json:"user_name,omitempty"`
	Error    string `json:"error,omitempty"`
	// token is used server-side to delete the row precisely. It is
	// unexported, so it never reaches the JSON response.
	token string
}

// BotTokenCleanupResult is the outcome of a bot token cleanup run.
type BotTokenCleanupResult struct {
	TotalScanned int            `json:"total_scanned"`
	Valid        []BotTokenInfo `json:"valid"`
	Invalid      []BotTokenInfo `json:"invalid"`
	Kept         []BotTokenInfo `json:"kept"`
}
