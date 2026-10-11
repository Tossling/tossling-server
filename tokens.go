package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/netip"
	"sort"
	"sync"
	"time"

	"heckel.io/ntfy/v2/user"
)

const roomTokenLimit = 60

var (
	neverExpires      = time.Unix(0, 0)
	errRoomTokenLimit = errors.New("the server already keeps 60 room tokens; remove unused ones under Security in the server panel")
	roomTokenLock     sync.Mutex
)

type roomToken struct {
	ID          string
	Label       string
	LastUsed    time.Time
	LastOrigin  string
	Expires     time.Time
	Provisioned bool
}

type tokenResponse struct {
	Token      string `json:"token"`
	Label      string `json:"label,omitempty"`
	LastAccess int64  `json:"last_access,omitempty"`
	LastOrigin string `json:"last_origin,omitempty"`
	Expires    int64  `json:"expires,omitempty"`
}

func roomTokenID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func expiring(t *user.Token) bool {
	return !t.Provisioned && t.Expires.Unix() > 0
}

func issueRoomToken(m *user.Manager, label string, expires time.Time, origin netip.Addr) (*user.Token, error) {
	roomTokenLock.Lock()
	defer roomTokenLock.Unlock()
	u, err := m.User(deviceUser)
	if err != nil {
		return nil, err
	}
	tokens, err := m.Tokens(u.ID)
	if err != nil {
		return nil, err
	}
	kept := 0
	for _, t := range tokens {
		if expiring(t) && t.Expires.Before(time.Now()) {
			if err := m.RemoveToken(u.ID, t.Value); err != nil {
				return nil, err
			}
			continue
		}
		kept++
	}
	if kept >= roomTokenLimit {
		return nil, errRoomTokenLimit
	}
	return m.CreateToken(u.ID, label, expires, origin, false)
}

func keepRoomTokens(m *user.Manager) (int, error) {
	u, err := m.User(deviceUser)
	if err != nil {
		return 0, err
	}
	tokens, err := m.Tokens(u.ID)
	if err != nil {
		return 0, err
	}
	kept := 0
	for _, t := range tokens {
		if !expiring(t) || t.Expires.Before(time.Now()) {
			continue
		}
		if _, err := m.ChangeToken(u.ID, t.Value, nil, &neverExpires); err != nil {
			return kept, err
		}
		kept++
	}
	return kept, nil
}

func roomTokens(m *user.Manager) ([]roomToken, error) {
	u, err := m.User(deviceUser)
	if err != nil {
		return nil, err
	}
	tokens, err := m.Tokens(u.ID)
	if err != nil {
		return nil, err
	}
	out := make([]roomToken, 0, len(tokens))
	for _, t := range tokens {
		if expiring(t) && t.Expires.Before(time.Now()) {
			continue
		}
		info := roomToken{ID: roomTokenID(t.Value), Label: t.Label, Expires: t.Expires, Provisioned: t.Provisioned}
		if t.LastAccess.Unix() > 0 {
			info.LastUsed = t.LastAccess
		}
		if t.LastOrigin.IsValid() && !t.LastOrigin.IsUnspecified() && t.LastOrigin.String() != internalIP {
			info.LastOrigin = t.LastOrigin.String()
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provisioned != out[j].Provisioned {
			return out[i].Provisioned
		}
		return out[i].LastUsed.After(out[j].LastUsed)
	})
	return out, nil
}

func removeRoomToken(m *user.Manager, id string) error {
	u, err := m.User(deviceUser)
	if err != nil {
		return err
	}
	tokens, err := m.Tokens(u.ID)
	if err != nil {
		return err
	}
	for _, t := range tokens {
		if roomTokenID(t.Value) == id {
			return m.RemoveToken(u.ID, t.Value)
		}
	}
	return user.ErrTokenNotFound
}

func (a *adminPanel) issueToken(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil {
		apiError(w, http.StatusBadRequest, "cannot read the request")
		return
	}
	if len(body) > 4096 {
		apiError(w, http.StatusRequestEntityTooLarge, "the request is too large")
		return
	}
	var req struct {
		Label   *string `json:"label"`
		Expires *int64  `json:"expires"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			apiError(w, http.StatusBadRequest, "the request is not valid JSON")
			return
		}
	}
	label := ""
	if req.Label != nil {
		label = *req.Label
	}
	expires := neverExpires
	if req.Expires != nil {
		expires = time.Unix(*req.Expires, 0)
	}
	origin, err := netip.ParseAddr(clientIP(r, a.opts.behindProxy))
	if err != nil {
		origin = netip.IPv4Unspecified()
	}
	token, err := issueRoomToken(a.service.manager, label, expires, origin)
	if errors.Is(err, errRoomTokenLimit) {
		log.Printf("api: room token refused, the server keeps %d", roomTokenLimit)
		apiError(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		log.Printf("api: room token: %v", err)
		apiError(w, http.StatusInternalServerError, "cannot create the token")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		Token:      token.Value,
		Label:      token.Label,
		LastAccess: token.LastAccess.Unix(),
		LastOrigin: token.LastOrigin.String(),
		Expires:    token.Expires.Unix(),
	})
}
