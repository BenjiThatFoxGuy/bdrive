package services

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/pkg/models"
	"gorm.io/datatypes"
)

// ThumbnailSettingsResponse is the JSON shape for thumbnail/resizer settings.
type ThumbnailSettingsResponse struct {
	ResizerHost    string `json:"resizerHost"`
	ResizerWidth   int    `json:"resizerWidth"`
	ResizerQuality int    `json:"resizerQuality"`
}

// SettingsRouter returns a chi.Router with the instance settings endpoints.
// Mount it under /api/settings in the main router.
func (a *apiService) SettingsRouter() chi.Router {
	r := chi.NewRouter()

	r.Get("/thumbnail", a.handleGetThumbnailSettings)
	r.Put("/thumbnail", a.handlePutThumbnailSettings)

	return r
}

func (a *apiService) handleGetThumbnailSettings(w http.ResponseWriter, r *http.Request) {
	var setting models.InstanceSettings
	result := a.db.Where("key = ?", "thumbnail").First(&setting)

	resp := ThumbnailSettingsResponse{
		ResizerWidth:   360,
		ResizerQuality: 80,
	}

	if result.Error == nil {
		json.Unmarshal(setting.Value, &resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (a *apiService) handlePutThumbnailSettings(w http.ResponseWriter, r *http.Request) {
	userId := auth.GetUser(r.Context())
	if userId == 0 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req ThumbnailSettingsResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	valueBytes, _ := json.Marshal(req)

	setting := models.InstanceSettings{
		Key:   "thumbnail",
		Value: datatypes.JSON(valueBytes),
	}

	result := a.db.Where("key = ?", "thumbnail").Assign(setting).FirstOrCreate(&setting)
	if result.Error != nil {
		http.Error(w, `{"error":"failed to save settings"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(req)
}
