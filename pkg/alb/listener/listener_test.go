package listener

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	awselb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/coreos/alb-ingress-controller/pkg/annotations"
	"github.com/coreos/alb-ingress-controller/pkg/aws/elbv2"
	"github.com/coreos/alb-ingress-controller/pkg/util/log"
	"github.com/coreos/alb-ingress-controller/pkg/util/types"
)

const (
	newARN   = "arn1"
	newTg    = "tg1"
	newPort  = 8080
	newProto = "HTTP"
	newPort2 = 9000
)

var (
	logr      *log.Logger
	mockList1 *awselb.Listener
	mockList2 *awselb.Listener
	mockList3 *awselb.Listener
	rOpts1    *ReconcileOptions
)

func init() {
	elbv2.ELBV2svc = mockELBV2Client{}
	logr = log.New("test")

	rOpts1 = &ReconcileOptions{
		TargetGroups:    nil,
		LoadBalancerArn: nil,
		Eventf:          mockEventf,
	}
}

func setup() {
	mockList1 = &awselb.Listener{
		Port:     aws.Int64(newPort),
		Protocol: aws.String("HTTP"),
		DefaultActions: []*awselb.Action{{
			Type:           aws.String("default"),
			TargetGroupArn: aws.String(newTg),
		}},
	}

	mockList2 = &awselb.Listener{
		Port:     aws.Int64(newPort2),
		Protocol: aws.String("HTTP"),
		DefaultActions: []*awselb.Action{{
			Type:           aws.String("default"),
			TargetGroupArn: aws.String(newTg),
		}},
	}

	mockList3 = &awselb.Listener{
		Port:     aws.Int64(newPort),
		Protocol: aws.String("HTTPS"),
		Certificates: []*awselb.Certificate{
			{CertificateArn: aws.String("abc")},
		},
		DefaultActions: []*awselb.Action{{
			Type:           aws.String("default"),
			TargetGroupArn: aws.String(newTg),
		}},
	}
}

func TestNewHTTPListener(t *testing.T) {
	desiredPort := int64(newPort)
	o := &NewDesiredListenerOptions{
		Port:   annotations.PortData{desiredPort, "HTTP"},
		Logger: logr,
	}

	l := NewDesiredListener(o)

	desiredProto := "HTTP"
	if o.CertificateArn != nil {
		desiredProto = "HTTPS"
	}
	switch {
	case *l.Desired.Port != desiredPort:
		t.Errorf("Invalid port created. Actual: %d | Expected: %d", *l.Desired.Port,
			desiredPort)
	case *l.Desired.Protocol != desiredProto:
		t.Errorf("Invalid protocol created. Actual: %s | Expected: %s",
			*l.Desired.Protocol, desiredProto)
	}
}

func TestNewHTTPSListener(t *testing.T) {
	desiredPort := int64(443)
	desiredCertArn := aws.String("abc123")
	o := &NewDesiredListenerOptions{
		Port:           annotations.PortData{desiredPort, "HTTPS"},
		CertificateArn: desiredCertArn,
		Logger:         logr,
	}

	l := NewDesiredListener(o)

	desiredProto := "HTTP"
	if o.CertificateArn != nil {
		desiredProto = "HTTPS"
	}
	switch {
	case *l.Desired.Port != desiredPort:
		t.Errorf("Invalid port created. Actual: %d | Expected: %d", *l.Desired.Port,
			desiredPort)
	case *l.Desired.Protocol != desiredProto:
		t.Errorf("Invalid protocol created. Actual: %s | Expected: %s",
			*l.Desired.Protocol, desiredProto)
	case *l.Desired.Certificates[0].CertificateArn != *desiredCertArn:
		t.Errorf("Invalid certificate ARN. Actual: %s | Expected: %s",
			*l.Desired.Certificates[0].CertificateArn, *desiredCertArn)
	}
}

type mockELBV2Client struct {
	elbv2.ELBV2API
}

func (m mockELBV2Client) CreateListener(*awselb.CreateListenerInput) (*awselb.CreateListenerOutput, error) {
	o := &awselb.CreateListenerOutput{
		Listeners: []*awselb.Listener{
			{
				Port:        aws.Int64(newPort),
				ListenerArn: aws.String(newARN),
				Protocol:    aws.String(newProto),
			}},
	}
	return o, nil
}

func (m mockELBV2Client) RemoveListener(awselb.DeleteListenerInput) error {
	return nil
}

func (m mockELBV2Client) ModifyListener(*awselb.ModifyListenerInput) (*awselb.ModifyListenerOutput, error) {
	o := &awselb.ModifyListenerOutput{
		Listeners: []*awselb.Listener{
			{
				Port:        aws.Int64(newPort2),
				ListenerArn: aws.String(newARN),
				Protocol:    aws.String(newProto),
			}},
	}
	return o, nil
}

func mockEventf(a, b, c string, d ...interface{}) {
}

// TestReconcileCreate calls Reconcile on a mock Listener instance and assures creation is
// attempted.
func TestReconcileCreate(t *testing.T) {
	setup()
	l := Listener{
		Logger:  logr,
		Desired: mockList1,
	}

	l.Reconcile(rOpts1)

	if *l.Current.ListenerArn != newARN {
		t.Errorf("Listener arn not properly set. Actual: %s, Expected: %s", *l.Current.ListenerArn, newARN)
	}
	if types.DeepEqual(l.Desired, l.Current) {
		t.Error("After creation, desired and current listeners did not match.")
	}
}

// TestReconcileDelete calls Reconcile on a mock Listener instance and assures deletion is
// attempted.
func TestReconcileDelete(t *testing.T) {
	setup()
	elbv2.ELBV2svc = mockELBV2Client{}

	l := Listener{
		Logger:  logr,
		Current: mockList1,
	}

	l.Reconcile(rOpts1)

	if !l.Deleted {
		t.Error("Listener was deleted deleted flag was not set to true.")
	}

}

// TestReconcileModify calls Reconcile on a mock Listener instance and assures modification is
// attempted when the ports between a desired and current listener differ.
func TestReconcileModifyPortChange(t *testing.T) {
	setup()
	l := Listener{
		Logger:  logr,
		Desired: mockList2,
		Current: mockList1,
	}

	l.Reconcile(rOpts1)

	if *l.Current.Port != *l.Desired.Port {
		t.Errorf("Error. Current: %d | Desired: %d", *l.Current.Port, *l.Desired.Port)
	}
	if *l.Current.ListenerArn != newARN {
		t.Errorf("Listener arn not properly set. Actual: %s, Expected: %s", *l.Current.ListenerArn, newARN)
	}

}

// TestReconcileModify calls Reconcile on a mock Listener that contains an identical current and
// desired state. It expects no operation to be taken.
func TestReconcileModifyNoChange(t *testing.T) {
	setup()
	l := Listener{
		Logger:  logr,
		Desired: mockList2,
		Current: mockList1,
	}

	l.Desired.Port = mockList1.Port // this sets ports identical. Should prevent failure, if removed, test should fail.
	l.Reconcile(rOpts1)

	if *l.Current.Port != *mockList1.Port {
		t.Errorf("Error. Current: %d | Desired: %d", *l.Current.Port, *mockList1.Port)
	}
}

// TestModificationNeeds sends different listeners through to see if a modification is needed.
func TestModificationNeeds(t *testing.T) {
	setup()
	lPortNeedsMod := Listener{
		Logger:  logr,
		Desired: mockList2,
		Current: mockList1,
	}

	if !lPortNeedsMod.NeedsModificationCheck(lPortNeedsMod.Desired) {
		t.Error("Listener reported no modification needed. Ports were different and should" +
			"require modification")
	}

	lNoMod := Listener{
		Logger:  logr,
		Desired: mockList1,
		Current: mockList1,
	}

	if lNoMod.NeedsModificationCheck(lNoMod.Desired) {
		t.Error("Listener reported modification needed. Desired and Current were the same")
	}

	lCertNeedsMod := Listener{
		Logger:  logr,
		Desired: mockList3,
		Current: mockList1,
	}

	if !lCertNeedsMod.NeedsModificationCheck(lCertNeedsMod.Desired) {
		t.Error("Listener reported no modification needed. Certificates were different and" +
			"should require modification")
	}
}
