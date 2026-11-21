package handlers

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"hsg/internal/dtos"
	"hsg/internal/lib/apperr"
	"hsg/internal/repositories"
)

// KnowledgeUsageHandler exposes document usage metrics. Every tenant sees only
// its own usage; a super_user sees the global library usage.
type KnowledgeUsageHandler struct {
	usage *repositories.KnowledgeUsageRepository
}

func NewKnowledgeUsageHandler(usage *repositories.KnowledgeUsageRepository) *KnowledgeUsageHandler {
	return &KnowledgeUsageHandler{usage: usage}
}

// Window aggregates usage across the scope's documents.
func (h *KnowledgeUsageHandler) Window(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	from, to, err := usageWindow(c)
	if err != nil {
		return err
	}
	rows, err := h.usage.DocumentUsageWindow(c.Context(), uid, tenant, from, to)
	if err != nil {
		return apperr.New(apperr.KindInternal, "Gagal memuat metrik penggunaan")
	}
	return dtos.Get(c, rows)
}

// Document aggregates usage for one document.
func (h *KnowledgeUsageHandler) Document(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	docID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "ID dokumen tidak valid")
	}
	from, to, err := usageWindow(c)
	if err != nil {
		return err
	}
	row, err := h.usage.DocumentUsage(c.Context(), uid, tenant, docID, from, to)
	if err != nil {
		if err == apperr.ErrNotFound {
			return err
		}
		return apperr.New(apperr.KindInternal, "Gagal memuat metrik dokumen")
	}
	return dtos.Get(c, row)
}

func usageWindow(c fiber.Ctx) (time.Time, time.Time, error) {
	const defaultDays = 30
	days := defaultDays
	if raw := c.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 366 {
			return time.Time{}, time.Time{}, apperr.New(apperr.KindBadRequest, "days harus antara 1 dan 366")
		}
		days = parsed
	}
	now := time.Now().UTC()
	return now.AddDate(0, 0, -days), now, nil
}
