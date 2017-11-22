package listeners

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/coreos/alb-ingress-controller/pkg/annotations"
	"github.com/coreos/alb-ingress-controller/pkg/util/log"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	logger   *log.Logger
	ports    []int64
	schemes  []bool
	hosts    []string
	paths    []string
	svcs     []string
	svcPorts []int32
)

func init() {
	logger = log.New("test")
	ports = []int64{
		int64(80),
		int64(443),
		int64(8080),
	}
	schemes = []bool{
		false,
		true,
		false,
	}
	hosts = []string{
		"1.test.domain",
		"2.test.domain",
		"3.test.domain",
	}
	paths = []string{
		"/",
		"/store",
		"/store/dev",
	}
	svcs = []string{
		"1service",
		"2service",
		"3service",
	}
	svcPorts = []int32{
		int32(30001),
		int32(30002),
		int32(30003),
	}
}

func TestNewSingleListener(t *testing.T) {
	// mock ingress rules
	rs := []extensions.IngressRule{
		{
			Host: hosts[0],
			IngressRuleValue: extensions.IngressRuleValue{
				HTTP: &extensions.HTTPIngressRuleValue{
					Paths: []extensions.HTTPIngressPath{{
						Path: paths[0],
						Backend: extensions.IngressBackend{
							ServiceName: svcs[0],
							ServicePort: intstr.IntOrString{
								Type:   0,
								IntVal: svcPorts[0],
							},
						},
					},
					},
				},
			},
		},
	}

	// mock ingress options
	o := &NewDesiredListenersOptions{
		Annotations: &annotations.Annotations{
			Ports: []annotations.PortData{{ports[0], "HTTP"}},
		},
		Logger: logger,
		Ingress: &extensions.Ingress{
			Spec: extensions.IngressSpec{
				Rules: rs,
			},
		},
	}

	// validate expected listener results vs actual
	ls, err := NewDesiredListeners(o)
	if err != nil {
		t.Errorf("Failed to create listeners. Error: %s", err.Error())
	}
	expProto := "HTTP"
	if schemes[0] {
		expProto = "HTTPS"
	}

	switch {
	case len(ls) != 1:
		t.Errorf("Created %d listeners, should have been %d", len(ls), 1)
	case *ls[0].Desired.Port != ports[0]:
		t.Errorf("Port was %d should have been %d", *ls[0].Desired.Port, ports[0])
	case *ls[0].Desired.Protocol != expProto:
		t.Errorf("Invalid protocol was %s should have been %s", *ls[0].Desired.Protocol, expProto)
	case len(ls[0].Rules) != 2:
		t.Errorf("Quantity of rules attached to listener is invalid. Was %d, expected %d.", len(ls[0].Rules), 2)

	}
}

func TestMultipleListeners(t *testing.T) {
	as := &annotations.Annotations{}
	rs := []extensions.IngressRule{}

	// create annotations and listeners
	for i := range ports {
		as.Ports = append(as.Ports, annotations.PortData{ports[i], "HTTP"})
		if schemes[i] {
			as.Scheme = aws.String("HTTPS")
		}

		extRules := extensions.IngressRule{
			Host: hosts[i],
			IngressRuleValue: extensions.IngressRuleValue{
				HTTP: &extensions.HTTPIngressRuleValue{
					Paths: []extensions.HTTPIngressPath{{
						Path: paths[i],
						Backend: extensions.IngressBackend{
							ServiceName: svcs[i],
							ServicePort: intstr.IntOrString{
								Type:   0,
								IntVal: svcPorts[i],
							},
						},
					},
					},
				},
			},
		}
		rs = append(rs, extRules)
	}

	// mock ingress options
	o := &NewDesiredListenersOptions{
		Annotations: as,
		Logger:      logger,
		Ingress: &extensions.Ingress{
			Spec: extensions.IngressSpec{
				Rules: rs,
			},
		},
	}
	ls, err := NewDesiredListeners(o)
	if err != nil {
		t.Errorf("Failed to create listeners. Error: %s", err.Error())
	}

	// validate expected listener results vs actual
	for i := range as.Ports {
		// expProto := "HTTP"
		// if schemes[i] {
		// 	expProto = "HTTPS"
		// }

		switch {
		case len(ls) != len(ports):
			t.Errorf("Created %d listeners, should have been %d", len(ls), len(ports))
		case *ls[i].Desired.Port != ports[i]:
			t.Errorf("Port was %d should have been %d", *ls[i].Desired.Port, ports[i])
		// case *ls[i].Desired.Protocol != expProto:
		// t.Errorf("Invalid protocol was %s should have been %s", *ls[i].Desired.Protocol, expProto)
		case len(ls[i].Rules) != len(ports)+1:
			t.Errorf("Quantity of rules attached to listener is invalid. Was %d, expected %d.", len(ls[i].Rules), len(ports)+1)
		case !*ls[i].Rules[0].Desired.IsDefault:
			fmt.Println(awsutil.Prettify(ls[i].Rules))
			t.Errorf("1st rule wasn't marked as default rule.")
		case *ls[i].Rules[1].Desired.IsDefault:
			fmt.Println(awsutil.Prettify(ls[i].Rules))
			t.Errorf("2nd rule was marked as default, should only be the first")

		}
	}
}
