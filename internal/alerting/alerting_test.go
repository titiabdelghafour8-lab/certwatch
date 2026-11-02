package alerting

import (
	"reflect"
	"testing"

	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
)

func TestStatusFor(t *testing.T) {
	warn := []int{30, 14, 7}
	cases := map[int]model.Status{
		-1:  model.StatusExpired,
		0:   model.StatusCritical,
		7:   model.StatusCritical,
		8:   model.StatusWarning,
		30:  model.StatusWarning,
		31:  model.StatusOK,
		400: model.StatusOK,
	}
	for days, want := range cases {
		if got := StatusFor(days, warn); got != want {
			t.Errorf("StatusFor(%d) = %s, want %s", days, got, want)
		}
	}
}

func TestDueThreshold(t *testing.T) {
	warn := []int{30, 14, 7, 3, 1}
	cases := map[int]int{
		100: -1,
		31:  -1,
		30:  30,
		20:  30,
		14:  14,
		5:   7,
		1:   1,
		0:   1,
		-3:  ExpiredThreshold,
	}
	for days, want := range cases {
		if got := DueThreshold(days, warn); got != want {
			t.Errorf("DueThreshold(%d) = %d, want %d", days, got, want)
		}
	}
}

func TestNormalizeWarnDays(t *testing.T) {
	got, err := NormalizeWarnDays([]int{7, 30, 7, 1})
	if err != nil || !reflect.DeepEqual(got, []int{30, 7, 1}) {
		t.Errorf("got %v, %v", got, err)
	}
	if got, _ := NormalizeWarnDays(nil); !reflect.DeepEqual(got, model.DefaultWarnDays) {
		t.Errorf("defaults: got %v", got)
	}
	if _, err := NormalizeWarnDays([]int{0}); err == nil {
		t.Error("expected error for 0")
	}
	if _, err := NormalizeWarnDays([]int{400}); err == nil {
		t.Error("expected error for 400")
	}
}
