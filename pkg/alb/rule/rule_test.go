package rule

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"

	"github.com/coreos/alb-ingress-controller/pkg/alb/targetgroup"
	"github.com/coreos/alb-ingress-controller/pkg/alb/targetgroups"
	albelbv2 "github.com/coreos/alb-ingress-controller/pkg/aws/elbv2"
	"github.com/coreos/alb-ingress-controller/pkg/util/log"
)

func TestNewDesiredRule(t *testing.T) {
	cases := []struct {
		Priority     int
		Hostname     string
		Path         string
		SvcName      string
		ExpectedRule Rule
	}{
		{
			Priority: 0,
			Hostname: "hostname",
			Path:     "/path",
			SvcName:  "namespace-service",
			ExpectedRule: Rule{
				DesiredSvcName: "namespace-service",
				Desired: &elbv2.Rule{
					Priority:  aws.String("default"),
					IsDefault: aws.Bool(true),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
		},
		{
			Priority: 1,
			Hostname: "hostname",
			Path:     "/path",
			SvcName:  "namespace-service",
			ExpectedRule: Rule{
				DesiredSvcName: "namespace-service",
				Desired: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("host-header"),
							Values: []*string{aws.String("hostname")},
						},
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/path")},
						},
					},
					Actions: []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
		},
	}

	for i, c := range cases {
		rule := NewDesiredRule(&NewDesiredRuleOptions{
			Priority: c.Priority,
			Hostname: c.Hostname,
			Path:     c.Path,
			SvcName:  c.SvcName,
			Logger:   log.New("test"),
		})
		if log.Prettify(rule) != log.Prettify(c.ExpectedRule) {
			t.Errorf("TestNewDesiredRule.%v returned an unexpected rule:\n%s\n!=\n%s", i, log.Prettify(rule), log.Prettify(c.ExpectedRule))
		}
	}
}

func TestNewCurrentRule(t *testing.T) {
	r := &elbv2.Rule{RuleArn: aws.String("arn")}
	logger := log.New("test")

	newRule := NewCurrentRule(&NewCurrentRuleOptions{
		Rule:   r,
		Logger: logger,
	})

	if r != newRule.Current {
		t.Errorf("NewCurrentRule failed to set the Current to the rule argument")
	}
	if logger != newRule.logger {
		t.Errorf("NewCurrentRule failed to set the logger to the logger argument")
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		Rule             Rule
		Pass             bool
		CreateRuleOutput elbv2.CreateRuleOutput
		CreateRuleError  error
		ModifyRuleOutput elbv2.ModifyRuleOutput
		ModifyRuleError  error
		DeleteRuleOutput elbv2.DeleteRuleOutput
		DeleteRuleError  error
	}{
		{ // test empty rule, no current/desired rules
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
			},
			Pass: true,
		},
		{ // test Current is default, doesnt delete
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Current: &elbv2.Rule{
					Priority:  aws.String("default"),
					IsDefault: aws.Bool(true),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
			Pass: true,
		},
		{ // test delete
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Current: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
			Pass: true,
		},
		{ // test delete, fail
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Current: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
			DeleteRuleError: fmt.Errorf("fail"),
			Pass:            false,
		},
		{ // test desired rule is default, we do nothing
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Desired: &elbv2.Rule{
					Priority:  aws.String("default"),
					IsDefault: aws.Bool(true),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
			Pass: true,
		},
		{ // test current rule is nil, desired rule exists, runs create
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Desired: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
			CreateRuleOutput: elbv2.CreateRuleOutput{
				Rules: []*elbv2.Rule{
					&elbv2.Rule{
						Priority: aws.String("1"),
					},
				},
			},
			Pass: true,
		},
		{ // test current rule is nil, desired rule exists, runs create, fails
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Desired: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
				},
			},
			CreateRuleOutput: elbv2.CreateRuleOutput{
				Rules: []*elbv2.Rule{
					&elbv2.Rule{
						Priority: aws.String("1"),
					},
				},
			},
			CreateRuleError: fmt.Errorf("fail"),
			Pass:            false,
		},
		{ // test current rule and desired rule are different, modify current rule
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Current: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/path")},
						},
					},
				},
				Desired: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/otherpath")},
						},
					},
				},
			},
			ModifyRuleOutput: elbv2.ModifyRuleOutput{
				Rules: []*elbv2.Rule{
					&elbv2.Rule{
						Priority: aws.String("1"),
					},
				},
			},
			Pass: true,
		},
		{ // test current rule and desired rule are different, modify current rule, fail
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Current: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					RuleArn:   aws.String("arn"),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/path")},
						},
					},
				},
				Desired: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/otherpath")},
						},
					},
				},
			},
			ModifyRuleOutput: elbv2.ModifyRuleOutput{
				Rules: []*elbv2.Rule{
					&elbv2.Rule{
						Priority: aws.String("1"),
					},
				},
			},
			ModifyRuleError: fmt.Errorf("fail"),
			Pass:            false,
		},
		{ // test current rule and desired rule are the same, default case
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
				Current: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/path")},
						},
					},
				},
				Desired: &elbv2.Rule{
					Priority:  aws.String("1"),
					IsDefault: aws.Bool(false),
					Actions:   []*elbv2.Action{{Type: aws.String("forward")}},
					Conditions: []*elbv2.RuleCondition{
						{
							Field:  aws.String("path-pattern"),
							Values: []*string{aws.String("/path")},
						},
					},
				},
			},
			Pass: true,
		},
	}

	rOpts := &ReconcileOptions{
		ListenerArn: aws.String(":)"),
		TargetGroups: targetgroups.TargetGroups{
			&targetgroup.TargetGroup{
				SvcName: "namespace-service",
				Current: &elbv2.TargetGroup{
					TargetGroupArn: aws.String(":)"),
				},
			},
		},
		Eventf: mockEventf,
	}

	for i, c := range cases {
		albelbv2.ELBV2svc = mockedELBV2{
			CreateRuleOutput: c.CreateRuleOutput,
			ModifyRuleOutput: c.ModifyRuleOutput,
			DeleteRuleOutput: c.DeleteRuleOutput,
			CreateRuleError:  c.CreateRuleError,
			ModifyRuleError:  c.ModifyRuleError,
			DeleteRuleError:  c.DeleteRuleError,
		}
		err := c.Rule.Reconcile(rOpts)
		if err != nil && c.Pass {
			t.Errorf("rule.Reconcile.%v returned an error but should have succeeded.", i)
		}
		if err == nil && !c.Pass {
			t.Errorf("rule.Reconcile.%v succeeded but should have returned an error.", i)
		}
	}
}

func TestTargetGroupArn(t *testing.T) {
	cases := []struct {
		Expected     *string
		TargetGroups targetgroups.TargetGroups
		Rule         Rule
	}{
		{ // svcname is found in the targetgroups list, returns the targetgroup arn
			Expected: aws.String(":)"),
			TargetGroups: targetgroups.TargetGroups{
				&targetgroup.TargetGroup{
					SvcName: "namespace-service",
					Current: &elbv2.TargetGroup{
						TargetGroupArn: aws.String(":)"),
					},
				},
			},
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
			},
		},
		{ // svcname isn't found in targetgroups list, returns a nil
			Expected: nil,
			TargetGroups: targetgroups.TargetGroups{
				&targetgroup.TargetGroup{
					SvcName: "missing svc name",
				},
			},
			Rule: Rule{
				DesiredSvcName: "namespace-service",
				logger:         log.New("test"),
			},
		},
	}

	for i, c := range cases {
		s := c.Rule.TargetGroupArn(c.TargetGroups)
		if s == nil && c.Expected == nil {
			continue
		}
		if s == nil && c.Expected != nil {
			t.Errorf("rule.targetGroupArn.%v returned nil but should have returned '%s'.", i, *c.Expected)
			continue
		}
		if s != nil && c.Expected == nil {
			t.Errorf("rule.targetGroupArn.%v returned '%s' but should have returned nil.", i, *s)
			continue
		}
		if *s != *c.Expected {
			t.Errorf("rule.targetGroupArn.%v returned '%s' but should have returned '%s'.", i, *s, *c.Expected)
			continue
		}
	}
}

func TestCreate(t *testing.T) {
}

func TestModify(t *testing.T) {
}

func TestRuleDelete(t *testing.T) {
	cases := []struct {
		Priority             int
		Hostname             string
		Path                 string
		SvcName              string
		CopyDesiredToCurrent bool
		Pass                 bool
		DeleteRuleError      error
	}{
		{ // test Current == nil
			Priority:             1,
			Hostname:             "hostname",
			Path:                 "/path",
			SvcName:              "namespace-service",
			CopyDesiredToCurrent: false,
			Pass:                 true,
		},
		{ // test deleting a default rule
			Priority:             0,
			Hostname:             "hostname",
			Path:                 "/path",
			SvcName:              "namespace-service",
			CopyDesiredToCurrent: true,
			Pass:                 true,
		},
		{ // test a successful delete
			Priority:             1,
			Hostname:             "hostname",
			Path:                 "/path",
			SvcName:              "namespace-service",
			CopyDesiredToCurrent: true,
			Pass:                 true,
		},
		{ // test a delete that returns an error
			Priority:             1,
			Hostname:             "hostname",
			Path:                 "/path",
			SvcName:              "namespace-service",
			CopyDesiredToCurrent: true,
			DeleteRuleError:      fmt.Errorf("Failed deleting rule"),
			Pass:                 false,
		},
	}

	rOpts := &ReconcileOptions{
		ListenerArn:  aws.String(":)"),
		TargetGroups: nil,
		Eventf:       mockEventf,
	}

	for i, c := range cases {
		rule := NewDesiredRule(&NewDesiredRuleOptions{
			Priority: c.Priority,
			Hostname: c.Hostname,
			Path:     c.Path,
			SvcName:  c.SvcName,
			Logger:   log.New("test"),
		})

		albelbv2.ELBV2svc = mockedELBV2{
			DeleteRuleOutput: elbv2.DeleteRuleOutput{},
			DeleteRuleError:  c.DeleteRuleError,
		}

		if c.CopyDesiredToCurrent {
			rule.Current = rule.Desired
		}

		err := rule.delete(rOpts)
		if err != nil && c.Pass {
			t.Errorf("rule.delete.%v returned an error but should have succeeded.", i)
		}
		if err == nil && !c.Pass {
			t.Errorf("rule.delete.%v succeeded but should have returned an error.", i)
		}
	}
}

func TestNeedsModification(t *testing.T) {
	cases := []struct {
		NeedsModification bool
		Current           *elbv2.Rule
		Desired           *elbv2.Rule
	}{
		{ // new rule, current rule is empty
			NeedsModification: true,
			Desired: &elbv2.Rule{
				Conditions: []*elbv2.RuleCondition{
					{
						Field:  aws.String("host-header"),
						Values: []*string{aws.String("hostname")},
					},
					{
						Field:  aws.String("path-pattern"),
						Values: []*string{aws.String("/path")},
					},
				},
			},
		},
		{ // conditions removed from desired rule
			NeedsModification: true,
			Current: &elbv2.Rule{
				Conditions: []*elbv2.RuleCondition{
					{
						Field:  aws.String("host-header"),
						Values: []*string{aws.String("hostname")},
					},
					{
						Field:  aws.String("path-pattern"),
						Values: []*string{aws.String("/path")},
					},
				},
			},
			Desired: &elbv2.Rule{},
		},
		{ // conditions are the same
			NeedsModification: false,
			Current: &elbv2.Rule{
				Conditions: []*elbv2.RuleCondition{
					{
						Field:  aws.String("host-header"),
						Values: []*string{aws.String("hostname")},
					},
					{
						Field:  aws.String("path-pattern"),
						Values: []*string{aws.String("/path")},
					},
				},
			},
			Desired: &elbv2.Rule{
				Conditions: []*elbv2.RuleCondition{
					{
						Field:  aws.String("host-header"),
						Values: []*string{aws.String("hostname")},
					},
					{
						Field:  aws.String("path-pattern"),
						Values: []*string{aws.String("/path")},
					},
				},
			},
		},
		{ // conditions changed on desired rule
			NeedsModification: true,
			Current: &elbv2.Rule{
				Conditions: []*elbv2.RuleCondition{
					{
						Field:  aws.String("host-header"),
						Values: []*string{aws.String("hostname")},
					},
					{
						Field:  aws.String("path-pattern"),
						Values: []*string{aws.String("/path")},
					},
				},
			},
			Desired: &elbv2.Rule{
				Conditions: []*elbv2.RuleCondition{
					{
						Field:  aws.String("changed"),
						Values: []*string{aws.String("changed")},
					},
					{
						Field:  aws.String("changed"),
						Values: []*string{aws.String("changed")},
					},
				},
			},
		},
	}

	for i, c := range cases {
		rule := &Rule{
			logger:  log.New("test"),
			Current: c.Current,
			Desired: c.Desired,
		}

		if rule.needsModification() != c.NeedsModification {
			t.Errorf("rule.needsModification.%v returned %v but should have returned %v.", i, rule.needsModification(), c.NeedsModification)
		}
	}
}

func TestStripDesiredState(t *testing.T) {
	r := &Rule{Desired: &elbv2.Rule{}}

	r.StripDesiredState()

	if r.Desired != nil {
		t.Errorf("rule.StripDesiredState failed to strip the desired state from the rule")
	}
}

func TestStripCurrentState(t *testing.T) {
	r := &Rule{Current: &elbv2.Rule{}}

	r.StripCurrentState()

	if r.Current != nil {
		t.Errorf("rule.StripCurrentState failed to strip the current state from the rule")
	}
}

func TestPriority(t *testing.T) {
	cases := []struct {
		String string
		Int    int64
	}{
		{
			String: "default",
			Int:    0,
		},
		{
			String: "5",
			Int:    5,
		},
	}

	for i, c := range cases {
		out := priority(&c.String)
		if *out != c.Int {
			t.Errorf("rule.priority.%v returned %v but should have returned %v.", i, *out, c.Int)
		}
	}
}

func mockEventf(a, b, c string, d ...interface{}) {
}

type mockedELBV2 struct {
	albelbv2.ELBV2API
	CreateRuleOutput elbv2.CreateRuleOutput
	CreateRuleError  error
	ModifyRuleOutput elbv2.ModifyRuleOutput
	ModifyRuleError  error
	DeleteRuleOutput elbv2.DeleteRuleOutput
	DeleteRuleError  error
}

func (m mockedELBV2) CreateRule(input *elbv2.CreateRuleInput) (*elbv2.CreateRuleOutput, error) {
	return &m.CreateRuleOutput, m.CreateRuleError
}

func (m mockedELBV2) ModifyRule(input *elbv2.ModifyRuleInput) (*elbv2.ModifyRuleOutput, error) {
	return &m.ModifyRuleOutput, m.ModifyRuleError
}

func (m mockedELBV2) DeleteRule(input *elbv2.DeleteRuleInput) (*elbv2.DeleteRuleOutput, error) {
	return &m.DeleteRuleOutput, m.DeleteRuleError
}
