package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/installer"
)

// The controller writes a clone's enrolment to one path and the unit
// `zoomies agent install` leaves in the template reads another only if the two
// packages drift, which is a fleet of machines that boot and never join.
func TestTheEnrolmentFileIsWhereTheTemplatesUnitReadsIt(t *testing.T) {
	if machineEnvPath != installer.MachineEnvPath {
		t.Fatalf("controller writes %s, the template's unit reads %s", machineEnvPath, installer.MachineEnvPath)
	}
}
