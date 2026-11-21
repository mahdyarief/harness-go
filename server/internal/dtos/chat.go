package dtos

import "hsg/internal/lib/validator"

// CreateChatSessionBody names a new conversation. The title is optional; an
// empty title is derived from the first user message.
type CreateChatSessionBody struct {
	Title string `json:"title"`
}

func (dto CreateChatSessionBody) Validate(v *validator.MapValidator) {
	v.Field("title").String().MaxLen(maxTitleLen)
}

// RenameChatSessionBody changes the title of an existing session.
type RenameChatSessionBody struct {
	Title string `json:"title"`
}

func (dto RenameChatSessionBody) Validate(v *validator.MapValidator) {
	v.Field("title").Required().String().MaxLen(maxTitleLen)
}

// ChatTurnBody is one user turn. Tenant and session come from the route, so
// the body cannot widen the scope. System messages are never accepted here:
// the server owns the instructions.
type ChatTurnBody struct {
	Content            string `json:"content"`
	Stream             *bool  `json:"stream"`
	UseWebSearch       *bool  `json:"use_web_search"`
	UseKnowledgeSearch *bool  `json:"use_knowledge_search"`
}

func (dto ChatTurnBody) Validate(v *validator.MapValidator) {
	v.Field("content").Required().String().MaxLen(maxChatContentLen)
}

// WantsStream defaults to true: streaming is the primary transport and the
// blocking path is the fallback.
func (dto ChatTurnBody) WantsStream() bool {
	return dto.Stream == nil || *dto.Stream
}

// WantsWebSearch forces the runner to require the web search tool when the
// integration is enabled. A missing or disabled Exa config makes the turn
// proceed normally without search rather than failing it.
func (dto ChatTurnBody) WantsWebSearch() bool {
	return dto.UseWebSearch != nil && *dto.UseWebSearch
}

// WantsKnowledgeSearch reports whether the knowledge tool should be offered.
// It defaults to on: knowledge is a core feature, so an older client that
// omits the field keeps working, and the toggle only removes it when the user
// explicitly turns it off.
func (dto ChatTurnBody) WantsKnowledgeSearch() bool {
	return dto.UseKnowledgeSearch == nil || *dto.UseKnowledgeSearch
}

const (
	maxTitleLen       = 200
	maxChatContentLen = 8000
)
