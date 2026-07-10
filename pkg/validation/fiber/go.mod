module github.com/Vilsol/lakta/pkg/validation/fiber

go 1.26.4

// Sibling modules (github.com/Vilsol/lakta core, gofiber/fiber/v3) resolve via
// go.work until the next tag; only the module-scoped validator/v10 is required
// here so it stays out of the root module graph.
require github.com/go-playground/validator/v10 v10.30.3
