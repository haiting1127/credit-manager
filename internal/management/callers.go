package management

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yuluo688/credit-manager/internal/money"
	"github.com/yuluo688/credit-manager/internal/service"
	"github.com/yuluo688/credit-manager/internal/store"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func createCaller(ctx context.Context, svc *service.Service, body []byte) (pluginapi.ManagementResponse, error) {
	var req struct {
		ID                   string `json:"id"`
		DisplayName          string `json:"display_name"`
		Enabled              *bool  `json:"enabled"`
		QuotaMicroUSD        *int64 `json:"quota_micro_usd"`
		MaxConcurrentRequests *int64 `json:"max_concurrent_requests"`
		RPMLimit             *int64 `json:"rpm_limit"`
		TPMLimit             *int64 `json:"tpm_limit"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonErr(http.StatusBadRequest, "invalid json"), nil
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	// quota_micro_usd / max_concurrent_requests / rpm_limit / tpm_limit:
	// 0 (default) = unlimited, matching key quota semantics.
	quota := int64(0)
	if req.QuotaMicroUSD != nil {
		quota = *req.QuotaMicroUSD
	}
	maxConcurrent := int64(0)
	if req.MaxConcurrentRequests != nil {
		maxConcurrent = *req.MaxConcurrentRequests
	}
	rpm := int64(0)
	if req.RPMLimit != nil {
		rpm = *req.RPMLimit
	}
	tpm := int64(0)
	if req.TPMLimit != nil {
		tpm = *req.TPMLimit
	}
	caller, err := svc.Store().CreateCaller(ctx, store.CallerSpec{
		ID: req.ID, DisplayName: req.DisplayName, QuotaMicroUSD: money.MicroUSD(quota), Enabled: enabled,
		MaxConcurrentRequests: maxConcurrent, RPMLimit: rpm, TPMLimit: tpm,
	})
	if err != nil {
		return jsonErr(http.StatusBadRequest, err.Error()), nil
	}
	return jsonOK(callerView(caller)), nil
}

func listCallers(ctx context.Context, svc *service.Service, query map[string][]string) (pluginapi.ManagementResponse, error) {
	limit := queryInt(query, "limit", 100)
	items, err := svc.Store().ListCallers(ctx, limit)
	if err != nil {
		return jsonErr(http.StatusInternalServerError, err.Error()), nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, callerView(item))
	}
	return jsonOK(map[string]any{"items": out}), nil
}

func setEnabled(ctx context.Context, svc *service.Service, body []byte) (pluginapi.ManagementResponse, error) {
	var req struct {
		CallerID string `json:"caller_id"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonErr(http.StatusBadRequest, "invalid json"), nil
	}
	if err := svc.Store().SetCallerEnabled(ctx, req.CallerID, req.Enabled); err != nil {
		return jsonErr(http.StatusBadRequest, err.Error()), nil
	}
	return jsonOK(map[string]any{"caller_id": req.CallerID, "enabled": req.Enabled}), nil
}

func updateCaller(ctx context.Context, svc *service.Service, body []byte) (pluginapi.ManagementResponse, error) {
	var req struct {
		CallerID              string  `json:"caller_id"`
		QuotaMicroUSD         *int64  `json:"quota_micro_usd"`
		MaxConcurrentRequests *int64  `json:"max_concurrent_requests"`
		RPMLimit              *int64  `json:"rpm_limit"`
		TPMLimit              *int64  `json:"tpm_limit"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonErr(http.StatusBadRequest, "invalid json"), nil
	}
	if strings.TrimSpace(req.CallerID) == "" {
		return jsonErr(http.StatusBadRequest, "caller_id is required"), nil
	}
	update := store.CallerUpdate{
		MaxConcurrentRequests: req.MaxConcurrentRequests,
		RPMLimit:              req.RPMLimit,
		TPMLimit:              req.TPMLimit,
	}
	if req.QuotaMicroUSD != nil {
		quota := money.MicroUSD(*req.QuotaMicroUSD)
		update.QuotaMicroUSD = &quota
	}
	caller, err := svc.Store().UpdateCaller(ctx, req.CallerID, update)
	if err != nil {
		return jsonErr(http.StatusBadRequest, err.Error()), nil
	}
	return jsonOK(callerView(caller)), nil
}
