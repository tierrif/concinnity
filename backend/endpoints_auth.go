package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
)

func IsAuthenticated(w http.ResponseWriter, r *http.Request) (*User, *Token) {
	token := r.Header.Get("Authentication")
	if cookie, err := r.Cookie("token"); err == nil {
		token = cookie.Value
	}
	if token == "" {
		if w != nil {
			http.Error(w, errorJson("You are not authenticated to access this resource!"),
				http.StatusUnauthorized)
		}
		return nil, nil
	}

	res, err := findUserByTokenStmt.Query(token)
	if err != nil {
		if w != nil {
			handleInternalServerError(w, err)
		}
		return nil, nil
	} else if !res.Next() {
		if w != nil {
			http.Error(w, errorJson("You are not authenticated to access this resource!"),
				http.StatusUnauthorized)
		}
		return nil, nil
	} else {
		var (
			username       string
			password       string
			email          string
			id             uuid.UUID
			userCreatedAt  time.Time
			verified       bool
			token          string
			tokenCreatedAt time.Time
		)
		err := res.Scan(&username, &password, &email, &id, &userCreatedAt, &verified, &token, &tokenCreatedAt)
		defer res.Close()
		if err != nil {
			if w != nil {
				handleInternalServerError(w, err)
			}
			return nil, nil
		}
		return &User{
				Username:  username,
				Password:  password,
				Email:     email,
				ID:        id,
				Verified:  verified,
				CreatedAt: userCreatedAt,
			}, &Token{
				CreatedAt: tokenCreatedAt,
				Token:     token,
				UserID:    id,
			}
	}
}

func StatusEndpoint(w http.ResponseWriter, r *http.Request) {
	if user, _ := IsAuthenticated(nil, r); user != nil {
		usernameJson, _ := json.Marshal(user.Username)
		w.Write([]byte("{\"online\":true,\"authenticated\":true,\"username\":" + string(usernameJson) + "}"))
	} else {
		w.Write([]byte("{\"online\":true,\"authenticated\":false}"))
	}
}

func LoginEndpoint(w http.ResponseWriter, r *http.Request) {
	// Check the body for JSON containing username and password and return a token.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, errorJson("Unable to read body!"), http.StatusBadRequest)
		return
	}
	var data struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, errorJson("Unable to read body!"), http.StatusBadRequest)
		return
	} else if data.Username == "" || data.Password == "" {
		http.Error(w, errorJson("No username or password provided!"), http.StatusBadRequest)
		return
	}
	var user User
	err = findUserByNameOrEmailStmt.QueryRow(data.Username, data.Username).Scan(
		&user.Username, &user.Password, &user.Email, &user.ID, &user.CreatedAt, &user.Verified)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		http.Error(w, errorJson("No account with this username/email exists!"), http.StatusUnauthorized)
		return
	} else if err != nil {
		handleInternalServerError(w, err)
		return
	} else if !user.Verified {
		http.Error(w, errorJson("Your account is not verified yet!"), http.StatusForbidden)
		return
	}
	tokenBytes := make([]byte, 64)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	result, err := insertTokenStmt.Exec(token, time.Now().UTC(), user.ID)
	if err != nil {
		handleInternalServerError(w, err)
		return
	} else if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		handleInternalServerError(w, err) // nil err solved by Ostrich algorithm
		return
	}
	// Add cookie to browser.
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		Secure:   config.SecureCookies,
		MaxAge:   3600 * 24 * 31,
		SameSite: http.SameSiteStrictMode,
	})
	json.NewEncoder(w).Encode(struct {
		Token    string `json:"token"`
		Username string `json:"username"`
	}{Token: token, Username: user.Username})
}

func LogoutEndpoint(w http.ResponseWriter, r *http.Request) {
	_, token := IsAuthenticated(w, r)
	if token == nil {
		return
	}
	result, err := deleteTokenStmt.Exec(token.Token)
	if err != nil {
		handleInternalServerError(w, err)
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		handleInternalServerError(w, err)
		return
	} else if rows == 0 {
		http.Error(w, errorJson("You are not authenticated to access this resource!"),
			http.StatusUnauthorized)
		return
	}
	// Delete cookie on browser.
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "null",
		HttpOnly: true,
		Secure:   config.SecureCookies,
		MaxAge:   -1,
		SameSite: http.SameSiteStrictMode,
	})
	w.Write([]byte("{\"success\":true}"))
}

func RegisterEndpoint(w http.ResponseWriter, r *http.Request) {
	// Check the body for JSON containing username, password and email, and return a token.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, errorJson("Unable to read body!"), http.StatusBadRequest)
		return
	}
	var data struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, errorJson("Unable to read body!"), http.StatusBadRequest)
		return
	} else if data.Username == "" || data.Password == "" || data.Email == "" {
		http.Error(w, errorJson("No username, e-mail or password provided!"), http.StatusBadRequest)
		return
	} else if data.Username == "system" { // Reserve this name to use in chat.
		http.Error(w, errorJson("An account with this e-mail already exists!"), http.StatusConflict)
		return
	} else if res, _ := regexp.MatchString("^[a-zA-Z0-9_]{4,16}$", data.Username); !res {
		http.Error(w, errorJson("Username should be 4-16 characters long, and "+
			"contain alphanumeric characters or _ only!"), http.StatusBadRequest)
		return
	} else if res, _ := regexp.MatchString("^.{8,64}$", data.Password); !res {
		http.Error(w, errorJson("Your password must be between 8 and 64 characters long!"),
			http.StatusBadRequest)
		return
	} else if res, _ := regexp.MatchString("^\\S+@\\S+\\.\\S+$", data.Email); !res {
		http.Error(w, errorJson("Invalid e-mail entered!"), http.StatusBadRequest)
		return
	}
	// Check if an account with this username or email already exists.
	var u User
	err = findUserByEmailStmt.QueryRow(data.Email).Scan(
		&u.Username, &u.Password, &u.Email, &u.ID, &u.CreatedAt, &u.Verified)
	if err == nil {
		http.Error(w, errorJson("An account with this e-mail already exists!"), http.StatusConflict)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		handleInternalServerError(w, err)
		return
	}
	err = findUserByUsernameStmt.QueryRow(data.Username).Scan(
		&u.Username, &u.Password, &u.Email, &u.ID, &u.CreatedAt, &u.Verified)
	if err == nil {
		http.Error(w, errorJson("An account with this username already exists!"), http.StatusConflict)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		handleInternalServerError(w, err)
		return
	}
	// Create the account.
	hash := HashPassword(data.Password, GenerateSalt())
	uuid := uuid.New()
	result, err := createUserStmt.Exec(data.Username, hash, data.Email, uuid)
	if err != nil {
		handleInternalServerError(w, err)
		return
	} else if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		handleInternalServerError(w, err) // nil err solved by Ostrich algorithm
		return
	}
	w.Write([]byte("{\"success\":true}"))
}
