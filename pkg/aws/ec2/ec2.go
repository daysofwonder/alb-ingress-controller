package ec2

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/karlseguin/ccache"
	"github.com/prometheus/client_golang/prometheus"

	albprom "github.com/coreos/alb-ingress-controller/pkg/prometheus"
)

const (
	instSpecifierTag = "instance"
	ManagedByKey     = "ManagedBy"
	ManagedByValue   = "alb-ingress"
)

// EC2svc is a pointer to the awsutil EC2 service
var EC2svc *EC2

// EC2 is our extension to AWS's ec2.EC2
type EC2 struct {
	ec2iface.EC2API
	cache *ccache.Cache
}

// NewEC2 returns an awsutil EC2 service
func NewEC2(awsSession *session.Session) {
	EC2svc = &EC2{
		ec2.New(awsSession),
		ccache.New(ccache.Configure()),
	}
}

// DescribeSGByPermissionGroup Finds an SG that the passed SG has permission to.
func (e *EC2) DescribeSGByPermissionGroup(sg *string) (*string, error) {
	in := &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("ip-permission.group-id"),
				Values: []*string{sg},
			},
		},
	}
	o, err := e.DescribeSecurityGroups(in)
	if err != nil {
		return nil, err
	}

	if len(o.SecurityGroups) != 1 {
		return nil, fmt.Errorf("Found more than 1 matching (managed) instance SGs. Found %d", len(o.SecurityGroups))
	}

	return o.SecurityGroups[0].GroupId, nil
}

// DescribeSGPorts returns the ports associated with a SG.
func (e *EC2) DescribeSGPorts(sgID *string) ([]int64, error) {
	in := &ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{sgID},
	}

	o, err := e.DescribeSecurityGroups(in)
	if err != nil || len(o.SecurityGroups) != 1 {
		return nil, err
	}

	ports := []int64{}
	for _, perm := range o.SecurityGroups[0].IpPermissions {
		ports = append(ports, *perm.FromPort)
	}

	return ports, nil
}

// DescribeSGTags returns tags for an sg when the sg-id is provided.
func (e *EC2) DescribeSGTags(sgID *string) ([]*ec2.TagDescription, error) {
	in := &ec2.DescribeTagsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("resource-id"),
				Values: []*string{sgID},
			},
		},
	}

	o, err := e.DescribeTags(in)
	if err != nil {
		return nil, err
	}

	return o.Tags, nil
}

// DeleteSecurityGroupByID deletes a security group based on its provided ID
func (e *EC2) DeleteSecurityGroupByID(sgID *string) error {
	in := &ec2.DeleteSecurityGroupInput{
		GroupId: sgID,
	}
	if _, err := e.DeleteSecurityGroup(in); err != nil {
		return err
	}

	return nil
}

// DisassociateSGFromInstanceIfNeeded loops through a list of instances to see if a managedSG
// exists. If it does, it attempts to remove the managedSG from the list.
func (e *EC2) DisassociateSGFromInstanceIfNeeded(instances []*string, managedSG *string) error {
	if managedSG == nil {
		return fmt.Errorf("Managed SG passed was empty unable to disassociate from instances.")
	}
	in := &ec2.DescribeInstancesInput{
		InstanceIds: instances,
	}

	for {
		insts, err := e.DescribeInstances(in)
		if err != nil {
			return err
		}

		// Compile the list of instances from which we will remove the ALB
		// security group in the next step.
		removeManagedSG := []*ec2.Instance{}
		for _, reservation := range insts.Reservations {
			for _, inst := range reservation.Instances {
				hasGroup := false
				for _, sg := range inst.SecurityGroups {
					if *managedSG == *sg.GroupId {
						hasGroup = true
					}
				}
				if hasGroup {
					removeManagedSG = append(removeManagedSG, inst)
				}
			}
		}

		for _, inst := range removeManagedSG {
			groups := []*string{}
			for _, sg := range inst.SecurityGroups {
				if *sg.GroupId != *managedSG {
					groups = append(groups, sg.GroupId)
				}
			}
			inAttr := &ec2.ModifyInstanceAttributeInput{
				InstanceId: inst.InstanceId,
				Groups:     groups,
			}
			if _, err := e.ModifyInstanceAttribute(inAttr); err != nil {
				return err
			}
		}

		if insts.NextToken == nil {
			break
		}

		in = &ec2.DescribeInstancesInput{
			NextToken: insts.NextToken,
		}
	}

	return nil
}

// AssociateSGToInstanceIfNeeded loops through a list of instances to see if newSG exists
// for them. It not, it is appended to the instances(s).
func (e *EC2) AssociateSGToInstanceIfNeeded(instances []*string, newSG *string) error {
	in := &ec2.DescribeInstancesInput{
		InstanceIds: instances,
	}

	for {
		insts, err := e.DescribeInstances(in)
		if err != nil {
			return err
		}

		// Compile the list of instances with the security group that
		// facilitates instance <-> ALB communication.
		needsManagedSG := []*ec2.Instance{}
		for _, reservation := range insts.Reservations {
			for _, inst := range reservation.Instances {
				hasGroup := false
				for _, sg := range inst.SecurityGroups {
					if *newSG == *sg.GroupId {
						hasGroup = true
					}
				}
				if !hasGroup {
					needsManagedSG = append(needsManagedSG, inst)
				}
			}
		}

		for _, inst := range needsManagedSG {
			groups := []*string{}
			for _, sg := range inst.SecurityGroups {
				groups = append(groups, sg.GroupId)
			}
			groups = append(groups, newSG)
			inAttr := &ec2.ModifyInstanceAttributeInput{
				InstanceId: inst.InstanceId,
				Groups:     groups,
			}
			if _, err := e.ModifyInstanceAttribute(inAttr); err != nil {
				return err
			}
		}

		if insts.NextToken == nil {
			break
		}

		in = &ec2.DescribeInstancesInput{
			NextToken: insts.NextToken,
		}
	}

	return nil
}

// UpdateSGIfNeeded attempts to resolve a security group based on its description.
// If one is found, it'll run an update that is effectivley a no-op when the groups are
// identical. Finally it'll attempt to find the associated instance SG and return that
// as the second string.
func (e *EC2) UpdateSGIfNeeded(vpcID *string, sgName *string, currentPorts []int64, desiredPorts []int64) (*string, *string, error) {
	// attempt to locate sg
	in := &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{vpcID},
			},
			{
				Name:   aws.String("group-name"),
				Values: []*string{sgName},
			},
		},
	}
	o, err := e.DescribeSecurityGroups(in)
	if err != nil {
		return nil, nil, err
	}

	// when no results were returned, security group doesn't exist and no need to attempt modification.
	if len(o.SecurityGroups) < 1 {
		return nil, nil, nil
	}
	groupId := o.SecurityGroups[0].GroupId

	// if no currentPorts were known to the LB but the sg stil resoled, query the SG to see if any ports can be resvoled
	if len(currentPorts) < 1 {
		currentPorts, err = e.DescribeSGPorts(groupId)
		if err != nil {
			return nil, nil, err
		}
	}

	// for each addPort, run an authorize to ensure it's added
	for _, port := range desiredPorts {
		if existsInOtherPortRange(port, currentPorts) {
			continue
		}
		in := &ec2.AuthorizeSecurityGroupIngressInput{
			GroupId: groupId,
			IpPermissions: []*ec2.IpPermission{
				{
					ToPort:     aws.Int64(port),
					FromPort:   aws.Int64(port),
					IpProtocol: aws.String("tcp"),
					IpRanges: []*ec2.IpRange{
						&ec2.IpRange{
							CidrIp:      aws.String("0.0.0.0/0"),
							Description: aws.String("Allow all inbound traffic."),
						},
					},
				},
			},
		}
		_, err := e.AuthorizeSecurityGroupIngress(in)
		if err != nil {
			return nil, nil, err
		}
	}

	// for each currentPort, run a revoke to ensure it can be removed
	for _, port := range currentPorts {
		if existsInOtherPortRange(port, desiredPorts) {
			continue
		}
		in := &ec2.RevokeSecurityGroupIngressInput{
			GroupId: groupId,
			IpPermissions: []*ec2.IpPermission{
				{
					ToPort:     aws.Int64(port),
					FromPort:   aws.Int64(port),
					IpProtocol: aws.String("tcp"),
					IpRanges: []*ec2.IpRange{
						&ec2.IpRange{
							CidrIp:      aws.String("0.0.0.0/0"),
							Description: aws.String("Allow all inbound traffic."),
						},
					},
				},
			},
		}
		_, err := e.RevokeSecurityGroupIngress(in)
		if err != nil {
			return nil, nil, err
		}
	}

	// attempt to resolve instance sg
	instanceSGName := fmt.Sprintf("%s-%s", instSpecifierTag, *sgName)
	inInstance := &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{vpcID},
			},
			{
				Name:   aws.String("group-name"),
				Values: []*string{aws.String(instanceSGName)},
			},
		},
	}
	oInstance, err := e.DescribeSecurityGroups(inInstance)
	if err != nil {
		return nil, nil, err
	}

	// managed sg may have existed but instance sg didn't
	if len(oInstance.SecurityGroups) < 1 {
		return o.SecurityGroups[0].GroupId, nil, nil
	}

	return groupId, oInstance.SecurityGroups[0].GroupId, nil
}

func existsInOtherPortRange(a int64, list []int64) bool {
	for _, p := range list {
		if a == p {
			return true
		}
	}
	return false
}

// CreateSecurityGroupFromPorts generates a new security group in AWS based on a list of ports. If
// successful, it returns the security group ID.
func (e *EC2) CreateSecurityGroupFromPorts(vpcID *string, sgName *string, ports []int64) (*string, *string, error) {
	inSG := &ec2.CreateSecurityGroupInput{
		VpcId:       vpcID,
		GroupName:   sgName,
		Description: sgName,
	}
	oSG, err := e.CreateSecurityGroup(inSG)
	if err != nil {
		return nil, nil, err
	}

	inSGRule := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: oSG.GroupId,
	}

	// for every port specified, allow all tcp traffic.
	for _, port := range ports {
		newRule := &ec2.IpPermission{
			FromPort:   aws.Int64(port),
			ToPort:     aws.Int64(port),
			IpProtocol: aws.String("tcp"),
			IpRanges: []*ec2.IpRange{
				&ec2.IpRange{
					CidrIp:      aws.String("0.0.0.0/0"),
					Description: aws.String("Allow all inbound traffic."),
				},
			},
		}
		inSGRule.IpPermissions = append(inSGRule.IpPermissions, newRule)
	}

	_, err = e.AuthorizeSecurityGroupIngress(inSGRule)
	if err != nil {
		return nil, nil, err
	}

	// tag the newly create security group with a name and managed by key
	inTags := &ec2.CreateTagsInput{
		Resources: []*string{oSG.GroupId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: sgName,
			},
			{
				Key:   aws.String(ManagedByKey),
				Value: aws.String(ManagedByValue),
			},
		},
	}
	if _, err := e.CreateTags(inTags); err != nil {
		return nil, nil, err
	}

	instanceGroupID, err := e.CreateNewInstanceSG(sgName, oSG.GroupId, vpcID)
	if err != nil {
		return nil, nil, err
	}

	return oSG.GroupId, instanceGroupID, nil
}

func (e *EC2) CreateNewInstanceSG(sgName *string, sgID *string, vpcID *string) (*string, error) {
	// create SG associated with above ALB securty group to attach to instances
	instanceSGName := fmt.Sprintf("%s-%s", instSpecifierTag, *sgName)
	inSG := &ec2.CreateSecurityGroupInput{
		VpcId:       vpcID,
		GroupName:   aws.String(instanceSGName),
		Description: aws.String(instanceSGName),
	}
	oInstanceSG, err := e.CreateSecurityGroup(inSG)
	if err != nil {
		return nil, err
	}

	inSGRule := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: oInstanceSG.GroupId,
		IpPermissions: []*ec2.IpPermission{
			{
				IpProtocol: aws.String("tcp"),
				ToPort:     aws.Int64(65535),
				FromPort:   aws.Int64(0),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{
					{
						VpcId:   vpcID,
						GroupId: sgID,
					},
				},
			},
		},
	}
	_, err = e.AuthorizeSecurityGroupIngress(inSGRule)
	if err != nil {
		return nil, err
	}

	// tag the newly create security group with a name and managed by key
	inTags := &ec2.CreateTagsInput{
		Resources: []*string{oInstanceSG.GroupId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(instanceSGName),
			},
			{
				Key:   aws.String(ManagedByKey),
				Value: aws.String(ManagedByValue),
			},
		},
	}
	if _, err := e.CreateTags(inTags); err != nil {
		return nil, err
	}

	return oInstanceSG.GroupId, nil
}

// GetVPCID retrieves the VPC that the subnets passed are contained in.
func (e *EC2) GetVPCID(subnets []*string) (*string, error) {
	var vpc *string

	if len(subnets) == 0 {
		return nil, fmt.Errorf("Empty subnet list provided to getVPCID")
	}

	key := fmt.Sprintf("%s-vpc", *subnets[0])
	item := e.cache.Get(key)

	if item == nil {
		subnetInfo, err := e.DescribeSubnets(&ec2.DescribeSubnetsInput{
			SubnetIds: subnets,
		})
		if err != nil {
			return nil, err
		}

		if len(subnetInfo.Subnets) == 0 {
			return nil, fmt.Errorf("DescribeSubnets returned no subnets")
		}

		vpc = subnetInfo.Subnets[0].VpcId
		e.cache.Set(key, vpc, time.Minute*60)

		albprom.AWSCache.With(prometheus.Labels{"cache": "vpc", "action": "miss"}).Add(float64(1))
	} else {
		vpc = item.Value().(*string)
		albprom.AWSCache.With(prometheus.Labels{"cache": "vpc", "action": "hit"}).Add(float64(1))
	}

	return vpc, nil
}

// Status validates EC2 connectivity
func (e *EC2) Status() func() error {
	return func() error {
		in := &ec2.DescribeTagsInput{}
		in.SetMaxResults(6)

		if _, err := e.DescribeTags(in); err != nil {
			return err
		}
		return nil
	}
}
