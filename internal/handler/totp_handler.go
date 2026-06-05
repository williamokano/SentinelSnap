package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	totpsvc "github.com/williamokano/sentinelsnap/internal/totp"
)

// TOTPHandler handles MFA TOTP endpoints.
type TOTPHandler struct {
	svc *totpsvc.Service
}

// NewTOTPHandler creates a new TOTPHandler.
func NewTOTPHandler(svc *totpsvc.Service) *TOTPHandler {
	return &TOTPHandler{svc: svc}
}

// extractAccountID reads X-Account-ID header and parses it as int64.
// This is a temporary shim that auth middleware will replace once #16 is built.
func extractAccountID(r *http.Request) (int64, bool) {
	raw := r.Header.Get("X-Account-ID")
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// extractCode decodes a JSON body that contains a single "code" field.
// It writes a 400 response and returns ("", false) when the body is malformed or the code is empty.
func extractCode(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		writeError(w, http.StatusBadRequest, "request body must include a non-empty \"code\" field")
		return "", false
	}
	return body.Code, true
}

// Setup handles POST /mfa/totp/setup
func (h *TOTPHandler) Setup(w http.ResponseWriter, r *http.Request) {
	accountID, ok := extractAccountID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "X-Account-ID header is required and must be a valid integer")
		return
	}

	result, err := h.svc.Setup(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, totpsvc.ErrAlreadyConfirmed) {
			writeError(w, http.StatusConflict, "TOTP is already set up and confirmed for this account")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not set up TOTP")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// Confirm handles POST /mfa/totp/confirm
func (h *TOTPHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	accountID, ok := extractAccountID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "X-Account-ID header is required and must be a valid integer")
		return
	}

	code, ok := extractCode(w, r)
	if !ok {
		return
	}

	result, err := h.svc.Confirm(r.Context(), accountID, code)
	if err != nil {
		switch {
		case errors.Is(err, totpsvc.ErrNotFound):
			writeError(w, http.StatusNotFound, "TOTP setup not found; call POST /mfa/totp/setup first")
		case errors.Is(err, totpsvc.ErrAlreadyConfirmed):
			writeError(w, http.StatusConflict, "TOTP is already confirmed")
		case errors.Is(err, totpsvc.ErrInvalidCode):
			writeError(w, http.StatusUnprocessableEntity, "invalid TOTP code")
		default:
			writeError(w, http.StatusInternalServerError, "could not confirm TOTP")
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// Verify handles POST /mfa/totp/verify
func (h *TOTPHandler) Verify(w http.ResponseWriter, r *http.Request) {
	accountID, ok := extractAccountID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "X-Account-ID header is required and must be a valid integer")
		return
	}

	code, ok := extractCode(w, r)
	if !ok {
		return
	}

	if err := h.svc.Verify(r.Context(), accountID, code); err != nil {
		switch {
		case errors.Is(err, totpsvc.ErrNotFound):
			writeError(w, http.StatusNotFound, "TOTP not set up for this account")
		case errors.Is(err, totpsvc.ErrNotConfirmed):
			writeError(w, http.StatusUnprocessableEntity, "TOTP has not been confirmed yet")
		case errors.Is(err, totpsvc.ErrInvalidCode):
			writeError(w, http.StatusUnprocessableEntity, "invalid TOTP code")
		default:
			writeError(w, http.StatusInternalServerError, "could not verify TOTP")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Disable handles DELETE /mfa/totp
func (h *TOTPHandler) Disable(w http.ResponseWriter, r *http.Request) {
	accountID, ok := extractAccountID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "X-Account-ID header is required and must be a valid integer")
		return
	}

	code, ok := extractCode(w, r)
	if !ok {
		return
	}

	if err := h.svc.Disable(r.Context(), accountID, code); err != nil {
		switch {
		case errors.Is(err, totpsvc.ErrNotFound):
			writeError(w, http.StatusNotFound, "TOTP not set up for this account")
		case errors.Is(err, totpsvc.ErrNotConfirmed):
			writeError(w, http.StatusUnprocessableEntity, "TOTP has not been confirmed yet")
		case errors.Is(err, totpsvc.ErrInvalidCode):
			writeError(w, http.StatusUnprocessableEntity, "invalid TOTP code")
		default:
			writeError(w, http.StatusInternalServerError, "could not disable TOTP")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
