package desk

import (
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"reflect"
	"testing"
)

func TestStorageIdentityLaunchCompletedOriginalRepeat(t *testing.T) {
	for _, name := range []string{"exact", "same-id-plan-fork", "changed-initial", "changed-components", "unsettled", "incomplete", "duplicate", "missing", "extra-action", "foreign-plan", "missing-plan-commitment", "completed-move-retained", "pending-move", "move-original-commitment"} {
		t.Run(name, func(t *testing.T) {
			catalog, _, _ := ownerV5Fixture(t, true)
			plan := catalog.LineagePlans[0]
			initial := storageCompositionInitial{Mode: 2, ExpectedOld: plan.PrivateObject}
			initial.ExpectedOld.ObjectKind = 2
			initial.ExpectedOld.Object++
			actions, err := storageIdentityRequiredActions(plan, initial)
			storageTestMust(t, err)
			digest, err := storageOwnerV5PlanDigest(plan)
			storageTestMust(t, err)
			components := []storagebudget.Component{{Root: plan.PrivateRoot.ID, Physical: storagebudget.Vector{Bytes: 37, Files: 1}, SettlementPhysical: storagebudget.Vector{Bytes: 41}}}
			applications := map[byte][]storagebudget.Component{}
			l := &storageIdentityLaunch{peak: storagebudget.CapacityVector{Bytes: 101, Inodes: 3}, debt: storagebudget.Vector{Bytes: 103}, groups: []storageIdentityLaunchGroup{{key: storageIdentityLaunchKey{Plan: plan.PlanID}, spent: true}}}
			for _, action := range actions {
				applications[action] = append([]storagebudget.Component{}, components...)
				l.applications = append(l.applications, storageIdentityLaunchApplication{originalPlan: digest, initial: initial, plan: plan.PlanID, action: action, components: append([]storagebudget.Component{}, components...), settled: true, complete: true})
			}
			switch name {
			case "same-id-plan-fork":
				plan.HistoryIdentity = append(append([]byte{}, plan.HistoryIdentity...), 'x')
			case "changed-initial":
				initial.ExpectedOld.Object++
			case "changed-components":
				applications[actions[0]][0].Physical.Bytes++
			case "unsettled":
				l.applications[0].settled = false
			case "incomplete":
				l.applications[0].complete = false
			case "duplicate":
				l.applications = append(l.applications, l.applications[0])
			case "missing":
				l.applications = l.applications[1:]
			case "extra-action":
				a := l.applications[0]
				a.action = 255
				l.applications = append(l.applications, a)
			case "foreign-plan":
				l.applications[0].plan[0] ^= 1
			case "missing-plan-commitment":
				l.applications[0].originalPlan = storagebudget.Digest{}
			case "completed-move-retained", "pending-move", "move-original-commitment":
				a := storageIdentityLaunchApplication{plan: plan.PlanID, action: storageIdentityIntentMove, components: append([]storagebudget.Component{}, components...), settled: true, complete: true}
				if name == "pending-move" {
					a.complete = false
				}
				if name == "move-original-commitment" {
					a.originalPlan = digest
				}
				l.applications = append(l.applications, a)
			}
			beforeGroups := append([]storageIdentityLaunchGroup{}, l.groups...)
			beforeApps := append([]storageIdentityLaunchApplication{}, l.applications...)
			for i := range beforeApps {
				beforeApps[i].components = append([]storagebudget.Component{}, beforeApps[i].components...)
			}
			peak, debt := l.peak, l.debt
			if got := l.completedOriginalPlan(plan, initial, applications); got != (name == "exact" || name == "completed-move-retained") {
				t.Fatal("completed original equality", got)
			}
			if !reflect.DeepEqual(beforeGroups, l.groups) || !reflect.DeepEqual(beforeApps, l.applications) || peak != l.peak || debt != l.debt {
				t.Fatal("comparison reset original spend or application")
			}
		})
	}
}
