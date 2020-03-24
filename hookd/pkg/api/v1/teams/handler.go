package api_v1_teams

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/navikt/deployment/hookd/pkg/database"
	"github.com/navikt/deployment/hookd/pkg/middleware"
	log "github.com/sirupsen/logrus"
)

type TeamsHandler struct {
	APIKeyStorage database.Database
}
type Team struct {
	Team    string `json:"team"`
	GroupId string `json:"groupId"`
}

func (h *TeamsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	groups, ok := r.Context().Value("groups").([]string)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fields := middleware.RequestLogFields(r)
	logger := log.WithFields(fields)
	keys := make([]database.ApiKey, 0)
	for _, group := range groups {
		apiKeys, err := h.APIKeyStorage.ReadByGroupClaim(group)
		if err != nil {
			logger.Error(err)
		}
		if len(apiKeys) > 0 {
			for _, apiKey := range apiKeys {
				keys = append(keys, apiKey)
			}
		}
	}
	teams := make([]Team, 0)
	for _, v := range keys {
		t := Team{
			GroupId: v.GroupId,
			Team:    v.Team,
		}
		teams = append(teams, t)
	}

	w.WriteHeader(http.StatusOK)
	render.JSON(w, r, teams)
}
