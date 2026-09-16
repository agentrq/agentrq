// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"github.com/gofiber/fiber/v2"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// FromHTTPRequestToEnrolMachineRequestEntity reads an enrolment request.
//
// Returns nil for a body that cannot be parsed. Note what it does **not** do:
// read a user id. The caller is unauthenticated and the code decides whose
// machine this becomes — taking an account from the request would let anyone
// enrol a machine into anyone's account.
func FromHTTPRequestToEnrolMachineRequestEntity(c *fiber.Ctx) *entity.EnrolMachineRequest {
	var payload struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Version  string `json:"version"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return nil
	}
	return &entity.EnrolMachineRequest{
		Code:     payload.Code,
		Name:     payload.Name,
		Hostname: payload.Hostname,
		OS:       payload.OS,
		Arch:     payload.Arch,
		Version:  payload.Version,
	}
}
