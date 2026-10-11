package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/netip"
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
	if !a.opts.demo {
		mux.HandleFunc("POST "+prefix+"/devices", a.deviceOnly(a.issueDevice))
		mux.HandleFunc("DELETE "+prefix+"/devices/{id}", a.deviceOnly(a.removeDevice))
	}
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

func (a *adminPanel) issueDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	origin, err := netip.ParseAddr(clientIP(r, a.opts.behindProxy))
	if err != nil {
		origin = netip.IPv4Unspecified()
	}
	token, err := issueDeviceToken(a.service.manager, presented, req.ID, req.Name, origin)
	switch {
	case errors.Is(err, errDeviceID):
		apiError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, errRoomTokenLimit):
		log.Printf("api: device token refused, the server keeps %d", roomTokenLimit)
		apiError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		log.Printf("api: device token: %v", err)
		apiError(w, http.StatusInternalServerError, "cannot create the token")
		return
	}
	log.Printf("api: a token of its own for device %s", req.ID)
	writeJSON(w, http.StatusOK, tokenResponse{Token: token.Value, Label: token.Label, LastAccess: token.LastAccess.Unix(), LastOrigin: token.LastOrigin.String()})
}

func (a *adminPanel) removeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	removed, err := removeDeviceTokens(a.service.manager, id)
	if errors.Is(err, errDeviceID) {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		log.Printf("api: device %s: %v", id, err)
		apiError(w, http.StatusInternalServerError, "cannot remove the tokens")
		return
	}
	log.Printf("api: device %s cut off, %d token(s) removed", id, removed)
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
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
