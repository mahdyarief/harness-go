package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"hsg/internal/app"
	"hsg/internal/dtos"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/lib/blob"
	"hsg/internal/lib/kb"
	"hsg/internal/repositories"
)

type KnowledgeHandler struct {
	repo       *repositories.KnowledgeRepository
	store      blob.Store
	extractors *kb.Registry
	ingest     chan struct{}
}

func NewKnowledgeHandler(a *app.Application, store blob.Store, extractors *kb.Registry) *KnowledgeHandler {
	return &KnowledgeHandler{
		repo: repositories.NewKnowledgeRepository(a.TenantDB), store: store,
		extractors: extractors, ingest: make(chan struct{}, 1),
	}
}

func knowledgeScope(c fiber.Ctx) (uuid.UUID, *uuid.UUID, error) {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return uuid.Nil, nil, apperr.ErrUnauthorized
	}
	if raw := c.Params("tenant_id"); raw != "" {
		tenant, err := uuid.Parse(raw)
		if err != nil || tenant == uuid.Nil {
			return uuid.Nil, nil, apperr.New(apperr.KindBadRequest, "Tenant tidak valid")
		}
		if rawHeader := c.Get("X-Tenant-ID"); rawHeader != "" {
			headerTenant, headerErr := uuid.Parse(rawHeader)
			if headerErr != nil || headerTenant != tenant {
				return uuid.Nil, nil, apperr.New(apperr.KindBadRequest, "Tenant pada header dan path tidak cocok")
			}
		}
		return uid, &tenant, nil
	}
	return uid, nil, nil
}

func (h *KnowledgeHandler) List(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	var q dtos.KnowledgeQuery
	if err = lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()
	params := repositories.KnowledgeListParams{
		ListParams:    listParams(q.ListQuery),
		CategoryID:    q.CategoryID,
		Uncategorized: q.Uncategorized,
	}
	docs, total, err := h.repo.List(c.Context(), uid, tenant, params)
	if err != nil {
		return err
	}
	return dtos.List(c, docs, dtos.ListMeta(total, q.Offset, q.Limit))
}

func (h *KnowledgeHandler) Upload(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	if err = h.repo.CheckWrite(ctx, uid, tenant); err != nil {
		return err
	}
	select {
	case h.ingest <- struct{}{}:
		defer func() { <-h.ingest }()
	default:
		return apperr.New(apperr.KindTooManyRequests, "Pemrosesan dokumen sedang sibuk. Coba lagi setelah unggahan selesai")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "Pilih berkas yang akan diunggah")
	}
	if file.Size > kb.MaxUploadBytes {
		return knowledgeFileError(kb.ErrTooLarge)
	}
	title := strings.TrimSpace(c.FormValue("title"))
	if title == "" || !utf8.ValidString(title) || strings.ContainsRune(title, 0) || utf8.RuneCountInString(title) > dtos.MaxKnowledgeTitleLen {
		return apperr.New(apperr.KindBadRequest, "Judul wajib diisi, maksimum 200 karakter")
	}
	filename := path.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
	if !utf8.ValidString(filename) || strings.ContainsRune(filename, 0) || utf8.RuneCountInString(filename) > 255 {
		return apperr.New(apperr.KindBadRequest, "Nama berkas tidak valid atau terlalu panjang")
	}
	f, err := file.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, kb.MaxUploadBytes+1))
	if err != nil {
		return err
	}
	if len(data) > kb.MaxUploadBytes {
		return knowledgeFileError(kb.ErrTooLarge)
	}
	format, err := kb.DetectFormat(filename, data)
	if err != nil {
		return knowledgeFileError(err)
	}
	segments, extraction, err := h.extractors.ExtractWithMetadata(ctx, format, data)
	if err != nil {
		return knowledgeFileError(err)
	}
	chunks, err := kb.ChunkSegments(segments, kb.DefaultChunkOptions())
	if err != nil {
		return knowledgeFileError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return knowledgeFileError(kb.ErrEmpty)
	}
	chars := 0
	for _, segment := range segments {
		chars += utf8.RuneCountInString(segment.Text)
	}
	metadata, err := json.Marshal(extraction)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	// Setiap percobaan memiliki key sendiri agar rollback unggahan bersamaan tidak menghapus blob yang sudah aktif.
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(digest+uuid.NewString())))
	if err = h.store.Put(ctx, key, data); err != nil {
		return err
	}
	categoryID, err := optionalCategoryID(c.FormValue("category_id"))
	if err != nil {
		h.removeBlob(context.WithoutCancel(c.Context()), key)
		return err
	}
	doc, err := h.repo.Create(ctx, uid, tenant, repositories.KnowledgeInput{
		Title: title, Filename: filename, Format: format, SizeBytes: int64(len(data)),
		SHA256: digest, StorageKey: key, CharCount: chars, ExtractionMetadata: metadata,
		CategoryID: categoryID, Status: "draft",
	}, chunks)
	if err != nil {
		if !errors.Is(err, repositories.ErrKnowledgeCommitUnknown) {
			h.removeBlob(context.WithoutCancel(c.Context()), key)
		}
		return err
	}
	return dtos.Created(c, doc, "Dokumen diunggah sebagai draft; terbitkan setelah ditinjau")
}

func (h *KnowledgeHandler) Preview(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "dokumen")
	if err != nil {
		return err
	}
	var q dtos.KnowledgePreviewQuery
	if err := lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()
	preview, err := h.repo.Preview(c.Context(), uid, tenant, id, q.Offset, q.Limit)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return dtos.Response(c, fiber.StatusOK, preview, dtos.ListMeta(preview.Document.ChunkCount, q.Offset, q.Limit), "")
}

func (h *KnowledgeHandler) Delete(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "dokumen")
	if err != nil {
		return err
	}
	key, err := h.repo.Delete(c.Context(), uid, tenant, id)
	if err != nil {
		return err
	}
	h.removeBlob(context.WithoutCancel(c.Context()), key)
	return dtos.Message(c, fiber.StatusOK, "Dokumen dihapus")
}

func (h *KnowledgeHandler) removeBlob(ctx context.Context, key string) {
	if err := h.store.Delete(ctx, key); err != nil && !errors.Is(err, blob.ErrNotFound) {
		slog.Error("gagal membersihkan blob knowledge", "key", key, "error", err)
	}
}

func knowledgeFileError(err error) error {
	switch {
	case errors.Is(err, kb.ErrUnsupportedFormat):
		return apperr.New(apperr.KindBadRequest, "Format belum didukung. Gunakan MD, TXT, CSV, DOCX, XLSX, atau PDF")
	case errors.Is(err, kb.ErrTooLarge):
		return apperr.New(apperr.KindBadRequest, "Berkas melebihi batas ukuran, kompleksitas, atau hasil ekstraksi yang diizinkan (unggahan maksimum 3 MiB)")
	case errors.Is(err, kb.ErrEmpty):
		return apperr.New(apperr.KindBadRequest, "Berkas tidak berisi teks yang dapat diindeks")
	case errors.Is(err, kb.ErrCorrupt):
		return apperr.New(apperr.KindBadRequest, "Berkas rusak atau tidak dapat dibaca")
	default:
		return err
	}
}

// optionalCategoryID parses an empty-as-nil category reference from a form or
// JSON body. An absent value means "no category"; a malformed one is rejected.
func optionalCategoryID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil, apperr.New(apperr.KindBadRequest, "Kategori tidak valid")
	}
	return &id, nil
}

// SetCategory reassigns a document's category. An empty category_id clears it.
func (h *KnowledgeHandler) SetCategory(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "knowledge document")
	if err != nil {
		return err
	}
	var body dtos.KnowledgeCategoryAssignBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	doc, err := h.repo.SetCategory(c.Context(), uid, tenant, id, body.CategoryID)
	if err != nil {
		return err
	}
	return dtos.Get(c, doc)
}

func (h *KnowledgeHandler) SetStatus(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "knowledge document")
	if err != nil {
		return err
	}
	var body dtos.KnowledgeStatusBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	doc, err := h.repo.SetStatus(c.Context(), uid, tenant, id, body.Status)
	if err != nil {
		return err
	}
	return dtos.Get(c, doc)
}

func (h *KnowledgeHandler) ListCategories(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	var q dtos.ListQuery
	if err = lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()
	cats, total, err := h.repo.ListCategories(c.Context(), uid, tenant, listParams(q))
	if err != nil {
		return err
	}
	return dtos.List(c, cats, dtos.ListMeta(total, q.Offset, q.Limit))
}

func (h *KnowledgeHandler) CreateCategory(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	var body dtos.KnowledgeCategoryBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	cat, err := h.repo.CreateCategory(c.Context(), uid, tenant, repositories.KnowledgeCategoryInput{Name: body.Name})
	if err != nil {
		return err
	}
	return dtos.Created(c, cat, "Kategori dibuat")
}

func (h *KnowledgeHandler) RenameCategory(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "knowledge category")
	if err != nil {
		return err
	}
	var body dtos.KnowledgeCategoryBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	cat, err := h.repo.RenameCategory(c.Context(), uid, tenant, id, body.Name)
	if err != nil {
		return err
	}
	return dtos.Get(c, cat)
}

func (h *KnowledgeHandler) DeleteCategory(c fiber.Ctx) error {
	uid, tenant, err := knowledgeScope(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "knowledge category")
	if err != nil {
		return err
	}
	if err = h.repo.DeleteCategory(c.Context(), uid, tenant, id); err != nil {
		return err
	}
	return dtos.Message(c, fiber.StatusOK, "Kategori dihapus. Dokumen tetap tersimpan tanpa kategori")
}
