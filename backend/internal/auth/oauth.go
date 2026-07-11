package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
)

var (
	ErrIdentityTaken   = errors.New("identity already linked to another user")
	ErrEmailCollision  = errors.New("email already registered to another account")
	ErrInvalidIdentity = errors.New("provider identity missing id or email")
)

const stateCookieName = "cp_oauth_state"

type ProviderUser struct {
	ID            string
	Email         string
	DisplayName   string
	EmailVerified bool
}

type ProviderConfig struct {
	OAuth2      *oauth2.Config
	UserInfoURL string
	ParseUser   func(body []byte) (ProviderUser, error)
}

func DiscordProvider(creds config.OAuthCredentials, redirectURL string) *ProviderConfig {
	return &ProviderConfig{
		OAuth2: &oauth2.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			Endpoint:     endpoints.Discord,
			RedirectURL:  redirectURL,
			Scopes:       []string{"identify", "email"},
		},
		UserInfoURL: "https://discord.com/api/users/@me",
		ParseUser: func(body []byte) (ProviderUser, error) {
			var v struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Email    string `json:"email"`
				Verified bool   `json:"verified"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				return ProviderUser{}, err
			}
			return ProviderUser{ID: v.ID, Email: v.Email, DisplayName: v.Username, EmailVerified: v.Verified}, nil
		},
	}
}

func GoogleProvider(creds config.OAuthCredentials, redirectURL string) *ProviderConfig {
	return &ProviderConfig{
		OAuth2: &oauth2.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			Endpoint:     endpoints.Google,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
		},
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		ParseUser: func(body []byte) (ProviderUser, error) {
			var v struct {
				Sub           string `json:"sub"`
				Email         string `json:"email"`
				EmailVerified bool   `json:"email_verified"`
				Name          string `json:"name"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				return ProviderUser{}, err
			}
			return ProviderUser{ID: v.Sub, Email: v.Email, DisplayName: v.Name, EmailVerified: v.EmailVerified}, nil
		},
	}
}

type OAuth struct {
	q         *db.Queries
	sessions  *Sessions
	baseURL   string
	secure    bool
	providers map[string]*ProviderConfig
}

func NewOAuth(q *db.Queries, sessions *Sessions, baseURL string, secure bool, providers map[string]*ProviderConfig) *OAuth {
	return &OAuth{q: q, sessions: sessions, baseURL: baseURL, secure: secure, providers: providers}
}

// CompleteLogin resolves a provider identity to a local user, creating or
// linking as needed. linkTo is the logged-in user during explicit linking,
// uuid.Nil otherwise.
func (o *OAuth) CompleteLogin(ctx context.Context, provider string, pu ProviderUser, linkTo uuid.UUID) (uuid.UUID, error) {
	if pu.ID == "" || pu.Email == "" {
		return uuid.Nil, ErrInvalidIdentity
	}
	ident, err := o.q.GetOAuthIdentity(ctx, db.GetOAuthIdentityParams{Provider: provider, ProviderUserID: pu.ID})
	switch {
	case err == nil:
		if linkTo != uuid.Nil && ident.UserID != linkTo {
			return uuid.Nil, ErrIdentityTaken
		}
		return ident.UserID, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, err
	}

	userID := linkTo
	if userID == uuid.Nil {
		userID, err = o.matchOrCreateUser(ctx, pu)
		if err != nil {
			return uuid.Nil, err
		}
	}
	err = o.q.CreateOAuthIdentity(ctx, db.CreateOAuthIdentityParams{
		Provider:       provider,
		ProviderUserID: pu.ID,
		UserID:         userID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

func (o *OAuth) matchOrCreateUser(ctx context.Context, pu ProviderUser) (uuid.UUID, error) {
	existing, err := o.q.GetUserByEmail(ctx, pu.Email)
	switch {
	case err == nil:
		if !pu.EmailVerified {
			// An unverified provider email must never silently attach to (or
			// shadow-create alongside) an existing account's address.
			return uuid.Nil, ErrEmailCollision
		}
		if existing.EmailVerifiedAt != nil {
			// Already-verified local account: link as-is.
			return existing.ID, nil
		}
		// Local account exists but never verified this email — a classic
		// pre-account-hijack target. Wipe its password so the attacker's
		// credentials stop working, then mark the email verified and link.
		if err := o.q.SetPasswordHash(ctx, db.SetPasswordHashParams{PasswordHash: nil, ID: existing.ID}); err != nil {
			return uuid.Nil, err
		}
		if err := o.q.MarkEmailVerified(ctx, existing.ID); err != nil {
			return uuid.Nil, err
		}
		return existing.ID, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, err
	}

	var verifiedAt *time.Time
	if pu.EmailVerified {
		now := time.Now()
		verifiedAt = &now
	}
	u, err := o.q.CreateUser(ctx, db.CreateUserParams{
		Email:           pu.Email,
		DisplayName:     pu.DisplayName,
		EmailVerifiedAt: verifiedAt,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

// Routes returns browser-redirect endpoints, mounted outside /api.
func (o *OAuth) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/{provider}/start", o.handleStart)
	r.Get("/{provider}/callback", o.handleCallback)
	return r
}

func (o *OAuth) handleStart(w http.ResponseWriter, r *http.Request) {
	p, ok := o.providers[chi.URLParam(r, "provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, _, err := newToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	link := "0"
	if r.URL.Query().Get("link") == "1" {
		link = "1"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state + ":" + link,
		Path:     "/auth/oauth",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   o.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, p.OAuth2.AuthCodeURL(state), http.StatusFound)
}

func (o *OAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	p, ok := o.providers[provider]
	if !ok {
		http.NotFound(w, r)
		return
	}
	fail := func() { http.Redirect(w, r, o.baseURL+"/login?error=oauth", http.StatusFound) }

	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		fail()
		return
	}
	state, link, ok := strings.Cut(stateCookie.Value, ":")
	if !ok || state == "" {
		fail()
		return
	}
	if r.URL.Query().Get("state") != state {
		fail()
		return
	}

	token, err := p.OAuth2.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		fail()
		return
	}
	pu, err := o.fetchUser(r.Context(), p, token)
	if err != nil {
		fail()
		return
	}

	linkTo := uuid.Nil
	if link == "1" {
		if c, err := r.Cookie(SessionCookieName); err == nil {
			if uid, err := o.sessions.UserID(r.Context(), c.Value); err == nil {
				linkTo = uid
			}
		}
		if linkTo == uuid.Nil {
			fail()
			return
		}
	}

	userID, err := o.CompleteLogin(r.Context(), provider, pu, linkTo)
	if err != nil {
		if errors.Is(err, ErrEmailCollision) {
			http.Redirect(w, r, o.baseURL+"/login?error=email-taken", http.StatusFound)
			return
		}
		fail()
		return
	}
	cookie, err := o.sessions.Create(r.Context(), userID)
	if err != nil {
		fail()
		return
	}
	http.SetCookie(w, cookie)
	target := o.baseURL + "/"
	if link == "1" {
		target = o.baseURL + "/account?linked=" + provider
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (o *OAuth) fetchUser(ctx context.Context, p *ProviderConfig, token *oauth2.Token) (ProviderUser, error) {
	client := p.OAuth2.Client(ctx, token)
	resp, err := client.Get(p.UserInfoURL)
	if err != nil {
		return ProviderUser{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProviderUser{}, fmt.Errorf("userinfo status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ProviderUser{}, err
	}
	return p.ParseUser(body)
}
