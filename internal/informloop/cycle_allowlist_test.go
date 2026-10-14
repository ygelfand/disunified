package informloop

import (
	"context"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type cycleController struct{ indexes []int }

func (*cycleController) ApplyPorts(context.Context, []devicemodel.PortDesired) (int, error) {
	return 0, nil
}
func (c *cycleController) CyclePort(_ context.Context, index int) error {
	c.indexes = append(c.indexes, index)
	return nil
}

func TestPortCyclesRespectWriteAllowlist(t *testing.T) {
	controller := &cycleController{}
	loop, _, _ := newPendingLoop(t, controller, false, true)
	loop.cfg.ControlPorts = map[int]bool{6: true}
	loop.pendingCycles = []int{6, 9}
	loop.cyclePorts(context.Background())
	if len(controller.indexes) != 1 || controller.indexes[0] != 6 {
		t.Fatalf("cycled ports=%v", controller.indexes)
	}
}
