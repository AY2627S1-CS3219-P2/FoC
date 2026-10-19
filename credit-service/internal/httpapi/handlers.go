// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented OpenAPI ServerInterface handlers for accounts, transactions, reservations, adjustments, and health per sourceOfTruth.md Phase 9.
// Author review: PENDING - reviewer to complete

package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/gen"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Handler implements gen.ServerInterface for Credit Service.
type Handler struct {
	creditService *credit.Service
	healthCheck   HealthCheck
}

// NewHandler returns a new HTTP API handler.
func NewHandler(creditService *credit.Service, healthCheck HealthCheck) *Handler {
	return &Handler{
		creditService: creditService,
		healthCheck:   healthCheck,
	}
}

// GetLiveness checks process liveness.
func (h *Handler) GetLiveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, gen.HealthResponse{
		Status: gen.Ok,
	})
}

// GetReadiness checks service readiness and database connectivity.
func (h *Handler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	if h.healthCheck != nil {
		if err := h.healthCheck(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, gen.ErrorCodeSERVICEUNAVAILABLE, "service unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, gen.HealthResponse{
		Status: gen.Ok,
	})
}

// GetMyCreditAccount returns the authenticated user's credit balance aggregate.
func (h *Handler) GetMyCreditAccount(w http.ResponseWriter, r *http.Request) {
	identity, ok := IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.ErrorCodeINVALIDREQUEST, "missing caller identity")
		return
	}

	userID, err := uuid.Parse(identity.UserID)
	if err != nil || userID == uuid.Nil {
		writeError(w, http.StatusUnauthorized, gen.ErrorCodeINVALIDREQUEST, "invalid caller identity")
		return
	}

	account, err := h.creditService.GetAccount(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, credit.ErrWalletNotReady):
			writeError(w, http.StatusConflict, gen.ErrorCodeWALLETNOTREADY, "wallet is not ready")
		default:
			writeError(w, http.StatusServiceUnavailable, gen.ErrorCodeSERVICEUNAVAILABLE, "service unavailable")
		}
		return
	}

	resp := gen.AccountResponse{
		UserId:    account.UserID,
		UpdatedAt: account.UpdatedAt,
	}

	if account.IntegrityStatus == credit.IntegrityConsistent {
		resp.CreditStatus = gen.READY
		resp.AvailableCredits = &account.AvailableCredits
		resp.ReservedCredits = &account.ReservedCredits
	} else {
		resp.CreditStatus = gen.TEMPORARILYUNAVAILABLE
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListMyCreditTransactions returns cursor-paginated grouped operations for the authenticated user.
func (h *Handler) ListMyCreditTransactions(w http.ResponseWriter, r *http.Request, params gen.ListMyCreditTransactionsParams) {
	identity, ok := IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.ErrorCodeINVALIDREQUEST, "missing caller identity")
		return
	}

	userID, err := uuid.Parse(identity.UserID)
	if err != nil || userID == uuid.Nil {
		writeError(w, http.StatusUnauthorized, gen.ErrorCodeINVALIDREQUEST, "invalid caller identity")
		return
	}

	limit := 20
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 100 {
			writeError(w, http.StatusBadRequest, gen.ErrorCodeINVALIDREQUEST, "limit must be between 1 and 100")
			return
		}
		limit = *params.Limit
	}

	paginated, err := h.creditService.ListTransactions(r.Context(), userID, limit, params.Cursor)
	if err != nil {
		switch {
		case errors.Is(err, credit.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, gen.ErrorCodeINVALIDREQUEST, err.Error())
		case errors.Is(err, credit.ErrAccountIntegrityFlagged):
			writeError(w, http.StatusConflict, gen.ErrorCodeACCOUNTINTEGRITYFAILURE, "account integrity failure")
		default:
			writeError(w, http.StatusInternalServerError, gen.ErrorCodeINTERNALERROR, "failed to list transactions")
		}
		return
	}

	items := make([]gen.TransactionOperation, len(paginated.Items))
	for i, it := range paginated.Items {
		items[i] = gen.TransactionOperation{
			OperationId:    it.OperationID,
			ErrandId:       it.ErrandID,
			Reason:         gen.TransactionReason(it.Reason),
			AvailableDelta: it.AvailableDelta,
			ReservedDelta:  it.ReservedDelta,
			OccurredAt:     it.OccurredAt,
			CreatedAt:      it.CreatedAt,
		}
	}

	writeJSON(w, http.StatusOK, gen.TransactionListResponse{
		Items:      items,
		NextCursor: paginated.NextCursor,
	})
}

// CreateReservation handles private-network reservation creation requests from Order Service.
func (h *Handler) CreateReservation(w http.ResponseWriter, r *http.Request) {
	req, err := decodeStrictJSON[gen.CreateReservationRequest](w, r)
	if err != nil {
		h.mapDomainError(w, err)
		return
	}

	if req.ErrandId == uuid.Nil || req.RequesterId == uuid.Nil {
		h.mapDomainError(w, fmt.Errorf("%w: nil UUID is not allowed", credit.ErrInvalidRequest))
		return
	}

	res, isNew, err := h.creditService.CreateReservation(r.Context(), req.ErrandId, req.RequesterId, req.Amount)
	if err != nil {
		h.mapDomainError(w, err)
		return
	}

	resp := gen.ReservationResponse{
		Id:          res.ID,
		ErrandId:    res.ErrandID,
		RequesterId: res.RequesterID,
		Amount:      res.Amount,
		Status:      gen.ReservationStatus(res.Status),
		CreatedAt:   res.CreatedAt,
		UpdatedAt:   res.UpdatedAt,
	}

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	writeJSON(w, status, resp)
}

// AdjustReservationReward handles private-network reward adjustments from Order Service.
func (h *Handler) AdjustReservationReward(w http.ResponseWriter, r *http.Request, errandId openapi_types.UUID) {
	if errandId == uuid.Nil {
		h.mapDomainError(w, fmt.Errorf("%w: nil errand ID", credit.ErrInvalidErrandID))
		return
	}

	req, err := decodeStrictJSON[gen.AdjustRewardRequest](w, r)
	if err != nil {
		h.mapDomainError(w, err)
		return
	}

	if req.OperationId == uuid.Nil {
		h.mapDomainError(w, fmt.Errorf("%w: nil operation ID", credit.ErrInvalidOperationID))
		return
	}

	_, res, err := h.creditService.AdjustReservationReward(r.Context(), errandId, req.CurrentAmount, req.NewAmount, req.OperationId)
	if err != nil {
		h.mapDomainError(w, err)
		return
	}

	resp := gen.ReservationResponse{
		Id:          res.ID,
		ErrandId:    res.ErrandID,
		RequesterId: res.RequesterID,
		Amount:      res.Amount,
		Status:      gen.ReservationStatus(res.Status),
		CreatedAt:   res.CreatedAt,
		UpdatedAt:   res.UpdatedAt,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) mapDomainError(w http.ResponseWriter, err error) {
	var amountErr *credit.AmountMismatchError
	if errors.As(err, &amountErr) {
		writeAmountMismatch(w, amountErr.CurrentAmount)
		return
	}

	switch {
	case errors.Is(err, credit.ErrInvalidRequest),
		errors.Is(err, credit.ErrInvalidRewardAmount),
		errors.Is(err, credit.ErrInvalidErrandID),
		errors.Is(err, credit.ErrInvalidUserID),
		errors.Is(err, credit.ErrInvalidOperationID):
		writeError(w, http.StatusBadRequest, gen.ErrorCodeINVALIDREQUEST, err.Error())
	case errors.Is(err, credit.ErrReservationNotFound):
		writeError(w, http.StatusNotFound, gen.ErrorCodeRESERVATIONNOTFOUND, "reservation not found")
	case errors.Is(err, credit.ErrReservationConflict),
		errors.Is(err, credit.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, gen.ErrorCodeIDEMPOTENCYCONFLICT, "idempotency conflict")
	case errors.Is(err, credit.ErrReservationStateConflict):
		writeError(w, http.StatusConflict, gen.ErrorCodeRESERVATIONSTATECONFLICT, "reservation in terminal state")
	case errors.Is(err, credit.ErrWalletNotReady):
		writeError(w, http.StatusConflict, gen.ErrorCodeWALLETNOTREADY, "wallet is not ready")
	case errors.Is(err, credit.ErrAccountIntegrityFlagged):
		writeError(w, http.StatusConflict, gen.ErrorCodeACCOUNTINTEGRITYFAILURE, "account integrity failure")
	case errors.Is(err, credit.ErrInsufficientCredits):
		writeError(w, http.StatusUnprocessableEntity, gen.ErrorCodeINSUFFICIENTCREDITS, "insufficient credits")
	case errors.Is(err, credit.ErrCreditLimitExceeded):
		writeError(w, http.StatusUnprocessableEntity, gen.ErrorCodeCREDITLIMITEXCEEDED, "credit limit exceeded")
	case errors.Is(err, credit.ErrServiceUnavailable):
		writeError(w, http.StatusServiceUnavailable, gen.ErrorCodeSERVICEUNAVAILABLE, "service unavailable")
	default:
		writeError(w, http.StatusInternalServerError, gen.ErrorCodeINTERNALERROR, "internal error")
	}
}

func decodeStrictJSON[T any](w http.ResponseWriter, r *http.Request) (*T, error) {
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return nil, fmt.Errorf("%w: Content-Type must be application/json", credit.ErrInvalidRequest)
	}

	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read request body (limit 16KiB): %v", credit.ErrInvalidRequest, err)
	}

	if err := validateJSONDuplicateKeys(bodyBytes); err != nil {
		return nil, fmt.Errorf("%w: %v", credit.ErrInvalidRequest, err)
	}

	dec := json.NewDecoder(bytes.NewReader(bodyBytes))
	dec.DisallowUnknownFields()

	var val T
	if err := dec.Decode(&val); err != nil {
		return nil, fmt.Errorf("%w: %v", credit.ErrInvalidRequest, err)
	}

	var trailing struct{}
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: multiple or trailing JSON values", credit.ErrInvalidRequest)
	}

	return &val, nil
}

func validateJSONDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	return parseJSONValue(dec)
}

func parseJSONValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := make(map[string]bool)
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("expected string object key")
			}
			if keys[key] {
				return fmt.Errorf("duplicate key %q", key)
			}
			keys[key] = true
			if err := parseJSONValue(dec); err != nil {
				return err
			}
		}
		// consume closing '}'
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if err := parseJSONValue(dec); err != nil {
				return err
			}
		}
		// consume closing ']'
		_, err = dec.Token()
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code gen.ErrorCode, message string) {
	writeJSON(w, status, gen.ErrorResponse{
		Code:    code,
		Message: message,
	})
}

func writeAmountMismatch(w http.ResponseWriter, currentAmount int64) {
	writeJSON(w, http.StatusConflict, gen.AmountMismatchErrorResponse{
		Code:    gen.AmountMismatchErrorResponseCodeRESERVATIONAMOUNTMISMATCH,
		Message: "reservation amount mismatch",
		Details: gen.AmountMismatchDetails{
			CurrentAmount: currentAmount,
		},
	})
}
