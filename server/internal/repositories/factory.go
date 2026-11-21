package repositories

import "database/sql"

// ChatRepository is not part of this struct: its tables are RLS-forced, so it
// must be built per request over the non-owner TenantDB via NewChatRepository.
// The owner connection used here would bypass the policy.
type Repositories struct {
	Category  CategoryRepository
	User      UserRepository
	LLMConfig LLMConfigRepository
	ExaConfig ExaConfigRepository
}

// New builds every repository over db.
func New(db *sql.DB) Repositories {
	base := BaseRepository{DB: db}

	return Repositories{
		Category:  CategoryRepository{base},
		User:      UserRepository{base},
		LLMConfig: LLMConfigRepository{base},
		ExaConfig: ExaConfigRepository{base},
	}
}
