package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type apiProject struct {
	Topic     string `json:"topic"`
	Name      string `json:"name"`
	Publisher string `json:"publisher,omitempty"`
	Token     string `json:"token,omitempty"`
	Example   string `json:"example,omitempty"`
}

func (a *adminPanel) deviceAuthed(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return false
	}
	u, err := a.service.manager.AuthenticateToken(token)
	return err == nil && u.Name == deviceUser
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (a *adminPanel) apiRoutes(mux *http.ServeMux) {
	for _, prefix := range apiPrefixes {
		a.apiRoutesAt(mux, prefix)
	}
}

func (a *adminPanel) apiRoutesAt(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("GET "+prefix+"/projects", a.deviceOnly(func(w http.ResponseWriter, r *http.Request) {
		projects, err := a.service.projects()
		if err != nil {
			apiError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]apiProject, 0, len(projects))
		for _, p := range projects {
			out = append(out, apiProject{Topic: p.Topic, Name: p.Name, Publisher: p.Publisher})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	mux.HandleFunc("POST "+prefix+"/projects", a.deviceOnly(func(w http.ResponseWriter, r *http.Request) {
		var req apiProject
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			apiError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		token, err := a.service.add(req.Topic, req.Name, req.Publisher)
		if err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("api: project %s created by a device", req.Topic)
		p, err := a.service.project(req.Topic)
		if err != nil {
			apiError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := apiProject{Topic: p.Topic, Name: p.Name, Publisher: p.Publisher, Token: token}
		if token != "" {
			out.Example = curlExample(a.opts.baseURL, p.Topic, token)
		}
		writeJSON(w, http.StatusCreated, out)
	}))
	mux.HandleFunc("DELETE "+prefix+"/projects/{topic}", a.deviceOnly(func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		if _, err := a.service.project(topic); err != nil {
			apiError(w, http.StatusNotFound, err.Error())
			return
		}
		if err := a.service.remove(topic); err != nil {
			apiError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("api: project %s removed by a device", topic)
		w.WriteHeader(http.StatusNoContent)
	}))
}

func (a *adminPanel) deviceOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.deviceAuthed(r) {
			apiError(w, http.StatusUnauthorized, "a device token is required")
			return
		}
		next(w, r)
	}
}
