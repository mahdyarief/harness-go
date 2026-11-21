package middlewares

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"hsg/internal/lib"
)

// RequestMeta copies the correlation id, client address and user agent onto the
// request context so repositories can record them on an audit row without
// importing fiber. It must run after requestid.
func RequestMeta(c fiber.Ctx) error {
	c.SetContext(lib.WithRequestMeta(c.Context(), lib.RequestMeta{
		RequestID: requestid.FromContext(c),
		IP:        c.IP(),
		UserAgent: c.Get(fiber.HeaderUserAgent),
	}))
	return c.Next()
}
