package handlers

import "hsg/internal/app"

type Handlers struct {
	Health   *healthHandler
	Category *categoryHandler
	Me       *meHandler
	LLM      *llmHandler
	Exa      *exaHandler
	User     *userHandler
	Chat     *chatHandler
	Token    *tokenHandler
	Audit    *auditHandler
	Review   *reviewHandler
}

func NewReviewHandler(app *app.Application) *reviewHandler {
	return newReviewHandler(app)
}

func New(app *app.Application) *Handlers {
	return &Handlers{
		Health:   &healthHandler{app: app},
		Category: &categoryHandler{app: app},
		Me:       &meHandler{app: app},
		LLM:      &llmHandler{app: app},
		Exa:      &exaHandler{app: app},
		User:     &userHandler{app: app},
		Chat:     &chatHandler{app: app},
		Token:    newTokenHandler(app),
		Audit:    newAuditHandler(app),
		Review:   newReviewHandler(app),
	}
}
