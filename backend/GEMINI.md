# AgentRQ Backend Coding Style and Naming Conventions

This document outlines the coding style and naming conventions for the AgentRQ backend workspace. AI assistants (like Gemini) should strictly adhere to these guidelines when generating or modifying code.

## 1. Workspace Structure

The workspace follows a standard Go layered architecture:
- `internal/handler/{transport}`: Transport layer handlers (e.g., Fiber HTTP handlers in `api`).
- `internal/controller/{domain}`: Business logic implementation (e.g., CRUD actions or domain-specific modules).
- `internal/data/model`: Database models (GORM) and table definitions.
- `internal/data/entity`: Domain entities passed between layers.
- `internal/data/view`: External representations (e.g., JSON request/response bodies).
- `internal/mapper`: Functions to map between models, entities, and views.
- `internal/repository`: Data access layer.

## 2. Naming Conventions

### Handlers (`internal/handler/{transport}/`)
Handlers process incoming HTTP requests and format responses.

- **Naming**: File names should match the entity being handled (e.g., `provider_tool.go`).
- **Structs**: The handler struct is typically named after the entity, e.g., `type ProviderTool struct { controller _crud.ProviderToolController }`.
- **Methods**: Use standard CRUD terminology: `Create`, `Get`, `List`, `Update`, `Delete`. They should match the `fiber.Handler` signature `func (c *fiber.Ctx) error`.
- **Flow**:
  1. Use a mapper to parse the request into an entity (e.g., `reqEntity := api.FromHTTPRequestTo<Action><Entity>RequestEntity(c)`).
  2. Return `fiber.ErrBadRequest` if the mapper returns `nil` (meaning parsing failed or a required param was missing).
  3. Call the corresponding `controller` method with `c.Context()` and the parsed request entity.
  4. Handle domain errors gracefully (e.g., checking for specific error types and returning appropriate HTTP status codes).
  5. Use a mapper to format the controller's response entity into a JSON payload byte array.
  6. Set `fiber.HeaderContentType` to `fiber.MIMEApplicationJSON` before returning `c.Send(payload)`.

### Controllers (`internal/controller/{domain}/`)
Controllers contain the core business logic.

- **Naming**: File names match the entity or domain logic (e.g., `provider_tool.go`).
- **Interface**: Define a `Controller` interface at the top of the file grouping the supported operations.
- **Implementation**: The struct implementing the interface should handle repository interactions, business rules, and validation.
- **Methods**: Methods typically accept a `context.Context` and a specific request entity (e.g., `*entity.CreateProviderToolRequest`), and return a specific response entity (e.g., `*entity.CreateProviderToolResponse`, `error`).

### Services (`internal/service/{domain}/`)
Services provide low-level utility or integration logic (e.g., SMTP, Image processing, Auth).

- **Methods**: Every service method MUST accept `ctx context.Context` as its FIRST parameter. This ensures consistent trace propagation and situational timeout management across the infrastructure.
- **Interface**: Define a `Service` interface to allow for situational mocking and dependency injection.

### Mappers (`internal/mapper/{transport}/`, `internal/mapper/model/`)
Mappers transform data between the different layers (View <-> Entity <-> Model). Focus on explicitly naming the source and target formats. Mappers must not return error. It can return `nil` if the conversion is not possible. The validation should be done in the controller/service layers.

- **Naming Constraints**:
  - Mapping mapped HTTP requests to Entities start with `FromHTTPRequestTo` and end with `RequestEntity`.
    - Example: `func FromHTTPRequestToCreateProviderToolRequestEntity(c *fiber.Ctx) *entity.CreateProviderToolRequest`
  - Mapping Entities to HTTP responses start with `From` and end with `ResponseEntityToHTTPResponse` (returning `[]byte`).
    - Example: `func FromCreateProviderToolResponseEntityToHTTPResponse(rs *entity.CreateProviderToolResponse) []byte`
  - Mapping internal structures start with `From` and explicitly state the source and target formats.
    - Example: `func FromProviderToolViewToProviderToolEntity(e view.ProviderTool) entity.ProviderTool`
    - Example: `func FromProviderToolEntitiesToProviderToolViews(es []entity.ProviderTool) []view.ProviderTool`
    - Example: `func FromProviderToolModelToProviderToolEntity(m model.ProviderTool) entity.ProviderTool`

### Entities (`internal/data/entity/`)
Entities are standard Go structs that specify the data passing between the controller layer and the handler/repository layers. Usually there's a `<Action><Entity>Request` and `<Action><Entity>Response` definition.

### Views (`internal/data/view/`)
Views define the external structures, like JSON inputs/outputs.
- Structs use `json:"fieldName"` struct tags with **camelCase** naming (e.g., `workspaceId`, `createdAt`).
- **CRITICAL**: No `snake_case` is allowed in the JSON API surface.
- Do not contain behavior/business logic or persistence details here.

## 3. General Best Practices
- **ID parsing:** Use `monoflake.IDFromBase62(c.Params("id")).Int64()` to parse base-62 ID strings from route parameters to standard int64 IDs.
- **Param validation:** Always check for missing route parameters inside `FromHTTPRequestTo...RequestEntity` and return `nil` if validation fails.
- **Payload transformation:** Return raw `[]byte` payloads from `...ToHTTPResponse` mappers using `json.Marshal` internally, preventing the caller (handler) from doing repetitive marshaling.
- **Formatting:** After your changes use always go format

## 4. Unit Testing and Mocking

Unit testing is critical for maintaining service reliability. 

- **Coverage**: Every new service addition or significant change MUST include unit tests. Aim for **100% code coverage** for the service layer logic.
- **Mocking**: Use `gomock` to mock external dependencies (repositories, other services, etc.).
  - Mocks are located in `internal/service/mocks/` and are excluded from version control.
  - Run `make mocks` from the root directory to generate or update mocks.
  - Run `make test` to execute all unit tests after generating mocks.
- **Test Placement**: Keep test files (`*_test.go`) in the same package as the service implementation to allow testing of unexported methods/fields when necessary for full coverage.