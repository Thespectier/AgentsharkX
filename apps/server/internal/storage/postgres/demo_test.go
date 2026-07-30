package postgres

import (
	"testing"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestDemoRunScanDoesNotFabricateApprovalSourceIdentity(t *testing.T) {
	t.Parallel()

	legacy := demoRunScanTarget{
		run:            model.DemoRun{SessionID: "demo-session"},
		approvalTicket: "opaque-ticket",
		approvalStatus: "pending",
	}
	run, err := legacy.value()
	if err != nil {
		t.Fatal(err)
	}
	if run.Approval != nil {
		t.Fatalf("legacy ticket columns fabricated an approval: %#v", run.Approval)
	}

	legacy.approval = []byte(`{"ticketId":"opaque-ticket","source":"agentguard","status":"pending"}`)
	run, err = legacy.value()
	if err != nil {
		t.Fatal(err)
	}
	if run.Approval != nil {
		t.Fatalf("incomplete approval JSON fabricated source identity: %#v", run.Approval)
	}
}
