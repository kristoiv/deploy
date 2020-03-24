package api_v1_apikey

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	api_v1 "github.com/navikt/deployment/hookd/pkg/api/v1"
	"github.com/navikt/deployment/hookd/pkg/database"
	"github.com/navikt/deployment/hookd/pkg/middleware"
	log "github.com/sirupsen/logrus"
)

type ApiKeyHandler struct {
	APIKeyStorage database.Database
}

// This method returns all the keys the user is authorized to see
func (h *ApiKeyHandler) GetApiKeys(w http.ResponseWriter, r *http.Request) {

	groups := r.Context().Value("groups").([]string)

	fields := middleware.RequestLogFields(r)
	logger := log.WithFields(fields)
	keys := make([]database.ApiKey, 0)

	for _, group := range groups {
		groupKeys, err := h.APIKeyStorage.ReadByGroupClaim(group)
		if err != nil {
			logger.Error(err)
		}
		if len(groupKeys) > 0 {
			for _, groupKey := range groupKeys {
				keys = append(keys, groupKey)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	render.JSON(w, r, keys)
}

// This method returns the deploy key for a specific team
func (h *ApiKeyHandler) GetTeamApiKey(w http.ResponseWriter, r *http.Request) {
	team := chi.URLParam(r, "team")
	groups := r.Context().Value("groups").([]string)

	fields := middleware.RequestLogFields(r)
	logger := log.WithFields(fields)
	apiKeys, err := h.APIKeyStorage.ReadAll(team, r.URL.Query().Get("limit"))

	if err != nil {
		logger.Errorln(err)
		if h.APIKeyStorage.IsErrNotFound(err) {
			w.WriteHeader(http.StatusNotFound)
			logger.Errorf("%s: %s", api_v1.FailedAuthenticationMsg, err)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		logger.Errorf("unable to fetch team apikey from storage: %s", err)
		return
	}

	keys := make([]database.ApiKey, 0)
	for _, apiKey := range apiKeys {
		for _, group := range groups {
			if group == apiKey.GroupId {
				keys = append(keys, apiKey)
			}
		}
	}

	if len(keys) == 0 {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("not authorized to view this team's keys"))
		return
	}

	w.WriteHeader(http.StatusOK)
	render.JSON(w, r, keys)
}

// This method returns the deploy apiKey for a specific team
func (h *ApiKeyHandler) RotateTeamApiKey(w http.ResponseWriter, r *http.Request) {
	team := chi.URLParam(r, "team")
	groups := r.Context().Value("groups").([]string)

	fields := middleware.RequestLogFields(r)
	logger := log.WithFields(fields)
	apiKeys, err := h.APIKeyStorage.Read(team)

	if err != nil {
		logger.Errorln(err)
		if h.APIKeyStorage.IsErrNotFound(err) {
			w.WriteHeader(http.StatusNotFound)
			logger.Errorf("%s: %s", "team does not exist", err)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		logger.Errorf("unable to fetch team apikey from storage: %s", err)
		return
	}

	var keyToRotate database.ApiKey
	for _, apiKey := range apiKeys {
		for _, group := range groups {
			if apiKey.GroupId == group && apiKey.Team == team {
				keyToRotate = apiKey
			}
		}
	}

	if keyToRotate.GroupId != "" {
		newKey, err := api_v1.Keygen(32)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Unable to generate new random apiKey"))
			return
		}
		logger.Infof("generated new apiKey for %s (%s)\n", keyToRotate.Team, keyToRotate.GroupId)
		err = h.APIKeyStorage.Write(keyToRotate.Team, keyToRotate.GroupId, newKey)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Unable to write new key"))
			return
		}

		apiKeys, err := h.APIKeyStorage.Read(team)
		keys := []database.ApiKey{}
		for _, apiKey := range apiKeys {
			for _, group := range groups {
				if group == apiKey.GroupId {
					keys = append(keys, apiKey)
				}
			}
		}
		if len(keys) > 0 {
			w.WriteHeader(http.StatusCreated)
			render.JSON(w, r, keys)
			return
		}
	}
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte("not allowed to rotate key"))
}
