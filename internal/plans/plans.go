// Package plans defines subscription tiers and their limits. Change limits
// here; the API enforces them and the CLI displays them.
package plans

import "github.com/titiabdelghafour8-lab/certwatch/internal/model"

// Unlimited marks a limit that is not enforced.
const Unlimited = -1

type Plan struct {
	Name        string
	MaxMonitors int
	MaxChannels int
}

var (
	Free       = Plan{Name: "free", MaxMonitors: 5, MaxChannels: 1}
	Pro        = Plan{Name: "pro", MaxMonitors: 100, MaxChannels: 10}
	Agency     = Plan{Name: "agency", MaxMonitors: 1000, MaxChannels: 50}
	SelfHosted = Plan{Name: "unlimited", MaxMonitors: Unlimited, MaxChannels: Unlimited}
)

var byName = map[string]Plan{
	Free.Name:       Free,
	Pro.Name:        Pro,
	Agency.Name:     Agency,
	SelfHosted.Name: SelfHosted,
}

// Paid lists plans that can be purchased, in display order.
var Paid = []Plan{Pro, Agency}

// Get returns the plan with the given name, falling back to Free.
func Get(name string) Plan {
	if p, ok := byName[name]; ok {
		return p
	}
	return Free
}

// Exists reports whether name is a known plan.
func Exists(name string) bool {
	_, ok := byName[name]
	return ok
}

// Allows reports whether current usage leaves room under limit.
func Allows(limit, current int) bool {
	return limit == Unlimited || current < limit
}

// Info converts a plan to its API representation.
func (p Plan) Info() model.PlanInfo {
	return model.PlanInfo{Name: p.Name, MaxMonitors: p.MaxMonitors, MaxChannels: p.MaxChannels}
}
