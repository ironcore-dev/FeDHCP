// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ironcore-dev/fedhcp/internal/kubernetes"

	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	origin = "fedhcp"
)

type K8sClient struct {
	Client        client.Client
	Namespace     string
	oobLabelValue string
	oobLabelKey   string
	EventRecorder record.EventRecorder
}

func NewK8sClient(namespace string, oobLabel string) (*K8sClient, error) {
	if !strings.Contains(oobLabel, "=") {
		return nil, fmt.Errorf("invalid subnet label: %s, should be 'key=value'", oobLabel)
	}

	cfg := kubernetes.GetConfig()
	cl := kubernetes.GetClient()

	corev1Client, err := corev1client.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create core client: %w", err)
	}

	broadcaster := record.NewBroadcaster()

	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: id})
	broadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: corev1Client.Events("")})

	labelKev, labelValue := strings.Split(oobLabel, "=")[0], strings.Split(oobLabel, "=")[1]
	k8sClient := K8sClient{
		Client:        cl,
		Namespace:     namespace,
		oobLabelKey:   labelKev,
		oobLabelValue: labelValue,
		EventRecorder: recorder,
	}

	return &k8sClient, nil
}

func (k K8sClient) getIp(
	ctx context.Context,
	ipaddr net.IP,
	mac net.HardwareAddr,
	exactIP bool,
	subnetType ipamv1alpha1.SubnetAddressType) (net.IP, error) {
	var ipamIP *ipamv1alpha1.IP
	macKey := strings.ReplaceAll(mac.String(), ":", "")

	subnetNames, err := k.getOOBNetworks(ctx, subnetType)
	if err != nil {
		return nil, fmt.Errorf("failed to get OOB networks: %w", err)
	}
	log.Debugf("%d OOB subnets found: %s", len(subnetNames), strings.Join(subnetNames, " "))

	// select the subnet matching the CIDR of the request
	subnetMatch := false
	for _, subnetName := range subnetNames {
		subnet, err := k.getMatchingSubnet(ctx, subnetName, ipaddr)
		if err != nil {
			log.Warningf("Error getting subnet %s/%s: %v", k.Namespace, subnetName, err)
			continue
		}
		if subnet == nil {
			continue
		}
		log.Debugf("Selecting subnet %s/%s", k.Namespace, subnetName)
		subnetMatch = true

		ipamIP, err = k.prepareCreateIpamIP(ctx, subnetName, macKey)
		if err != nil {
			return nil, err
		}
		if ipamIP == nil {
			ipamIP, err = k.doCreateIpamIP(ctx, subnetName, macKey, ipaddr, exactIP)
			if err != nil {
				return nil, err
			}
		} else {
			log.Debugf("Reserved IP %s (%s) already exists in subnet %s", ipamIP.Status.Reserved.String(),
				client.ObjectKeyFromObject(ipamIP), ipamIP.Spec.Subnet.Name)
			if err := k.applySubnetLabel(ctx, ipamIP); err != nil {
				return nil, err
			}
		}
		// break at first subnet match, there can be only one
		break
	}
	if !subnetMatch {
		return nil, fmt.Errorf("no matching subnet found for IP %s/%s", k.Namespace, ipaddr)
	}

	if ipamIP.Status.Reserved != nil {
		return net.ParseIP(ipamIP.Status.Reserved.String()), nil
	} else {
		return nil, fmt.Errorf("no reserved IP address found")
	}
}

func (k K8sClient) prepareCreateIpamIP(ctx context.Context, subnetName string, macKey string) (*ipamv1alpha1.IP, error) {
	ipList := &ipamv1alpha1.IPList{}
	if err := k.Client.List(ctx, ipList, client.InNamespace(k.Namespace), client.MatchingLabels{
		"mac": macKey,
	}); err != nil {
		return nil, fmt.Errorf("error listing IPs with MAC %v: %w", macKey, err)
	}

	for _, existingIpamIP := range ipList.Items {
		if existingIpamIP.Spec.Subnet.Name != subnetName {
			// IP with that MAC is assigned to a different subnet (v4 vs v6?)
			log.Debugf("IPAM IP with MAC %v and wrong subnet %s/%s found, ignoring", macKey,
				existingIpamIP.Namespace, existingIpamIP.Spec.Subnet.Name)
			continue
		} else if existingIpamIP.Status.State == ipamv1alpha1.FailedIPState {
			log.Infof("Failed IP %s in subnet %s found, deleting", client.ObjectKeyFromObject(&existingIpamIP), existingIpamIP.Spec.Subnet.Name)
			log.Debugf("Deleting old IP %s:\n%v", client.ObjectKeyFromObject(&existingIpamIP), prettyFormat(existingIpamIP.Status))
			if err := k.Client.Delete(ctx, &existingIpamIP); err != nil {
				return nil, fmt.Errorf("failed to delete IP %s: %w", client.ObjectKeyFromObject(&existingIpamIP), err)
			}

			if err := k.waitForDeletion(ctx, &existingIpamIP); err != nil {
				return nil, fmt.Errorf("failed to delete IP %s: %w", client.ObjectKeyFromObject(&existingIpamIP), err)
			}

			k.EventRecorder.Eventf(&existingIpamIP, corev1.EventTypeNormal, "Deleted", "Deleted old IPAM IP")
			log.Infof("Old IP %s deleted from subnet %s", client.ObjectKeyFromObject(&existingIpamIP), existingIpamIP.Spec.Subnet.Name)
		} else {
			// IP already exists
			return &existingIpamIP, nil
		}
	}

	return nil, nil
}

func (k K8sClient) doCreateIpamIP(ctx context.Context, subnetName string, macKey string, ipaddr net.IP, exactIP bool) (*ipamv1alpha1.IP, error) {
	var err error
	ipamIP := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: macKey + "-" + origin + "-",
			Namespace:    k.Namespace,
			Labels: map[string]string{
				"mac":         macKey,
				"origin":      origin,
				k.oobLabelKey: k.oobLabelValue,
			},
		},
		Spec: ipamv1alpha1.IPSpec{
			Subnet: corev1.LocalObjectReference{
				Name: subnetName,
			},
		},
	}

	if exactIP && ipaddr.String() != UNKNOWN_IP {
		ip, _ := ipamv1alpha1.IPAddrFromString(ipaddr.String())
		ipamIP.Spec.IP = ip
	}

	log.Infof("Creating new IP for MAC address %s", macKey)
	if err := k.Client.Create(ctx, ipamIP); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("failed to create IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
		} else {
			// do not create IP, because the deletion is not yet ready
			return nil, nil
		}
	}

	ipamIP, err = k.waitForCreation(ctx, ipamIP)
	if err != nil {
		return nil, fmt.Errorf("failed to create IP %w", err)
	} else {
		log.Infof("New IP %s (%s/%s) created in subnet %s", ipamIP.Status.Reserved.String(),
			ipamIP.Namespace, ipamIP.Name, ipamIP.Spec.Subnet.Name)
		k.EventRecorder.Eventf(ipamIP, corev1.EventTypeNormal, "Created", "Created IPAM IP")

		// update IP attributes
		createdIpamIP := ipamIP.DeepCopy()
		err := k.Client.Get(ctx, client.ObjectKeyFromObject(createdIpamIP), createdIpamIP)
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get IP %s: %w", client.ObjectKeyFromObject(createdIpamIP), err)
		}
		return createdIpamIP, nil
	}
}

func (k K8sClient) waitForDeletion(ctx context.Context, ipamIP *ipamv1alpha1.IP) error {
	if err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := k.Client.Get(ctx, client.ObjectKeyFromObject(ipamIP), ipamIP); err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			} else {
				// IP is deleted
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("timeout deleting IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
	}

	return nil
}

func (k K8sClient) waitForCreation(ctx context.Context, ipamIP *ipamv1alpha1.IP) (*ipamv1alpha1.IP, error) {
	if err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := k.Client.Get(ctx, client.ObjectKeyFromObject(ipamIP), ipamIP); err != nil {
			return false, err
		}
		if ipamIP.Status.State == ipamv1alpha1.FinishedIPState {
			return true, nil
		} else {
			return false, nil
		}
	}); err != nil {
		return nil, fmt.Errorf("timeout getting IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
	}

	return ipamIP, nil
}

func (k K8sClient) getOOBNetworks(ctx context.Context, subnetType ipamv1alpha1.SubnetAddressType) ([]string, error) {
	subnetList := &ipamv1alpha1.SubnetList{}
	if err := k.Client.List(ctx, subnetList, client.InNamespace(k.Namespace), client.MatchingLabels{
		k.oobLabelKey: k.oobLabelValue,
	}); err != nil {
		return nil, fmt.Errorf("error listing OOB subnets: %w", err)
	}

	var oobSubnetNames []string
	for _, subnet := range subnetList.Items {
		if subnet.Status.Type == subnetType {
			oobSubnetNames = append(oobSubnetNames, subnet.Name)
		}
	}

	return oobSubnetNames, nil
}

func (k K8sClient) getMatchingSubnet(ctx context.Context, subnetName string, ipaddr net.IP) (*ipamv1alpha1.Subnet, error) {
	subnet := &ipamv1alpha1.Subnet{}
	if err := k.Client.Get(ctx, types.NamespacedName{Name: subnetName, Namespace: k.Namespace}, subnet); apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("cannot select subnet %s, does not exist", client.ObjectKeyFromObject(subnet))
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get subnet %s: %w", client.ObjectKeyFromObject(subnet), err)
	}

	if !checkIPInCIDR(ipaddr, subnet.Status.Reserved.String()) && ipaddr.String() != UNKNOWN_IP {
		return nil, fmt.Errorf("cannot select subnet %s, CIDR mismatch", client.ObjectKeyFromObject(subnet))
	}

	return subnet, nil
}

func (k K8sClient) applySubnetLabel(ctx context.Context, ipamIP *ipamv1alpha1.IP) error {
	ipamIPBase := ipamIP.DeepCopy()
	ipamIP.Labels[k.oobLabelKey] = k.oobLabelValue
	if err := k.Client.Patch(ctx, ipamIP, client.MergeFrom(ipamIPBase)); err != nil {
		return fmt.Errorf("failed to patch IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
	}
	return nil
}

func prettyFormat(ipSpec interface{}) string {
	// Marshal the struct into a JSON string with pretty printing
	jsonBytes, err := json.MarshalIndent(ipSpec, "", "  ")
	if err != nil {
		log.Errorf("Error marshalling JSON: %v", err)
	}

	// Convert the JSON bytes to a string and print
	return string(jsonBytes)
}

func checkIPInCIDR(ip net.IP, cidrStr string) bool {
	// Parse the CIDR string
	_, cidrNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		log.Errorf("Error parsing CIDR: %v\n", err)
		return false
	}

	// Check if the CIDR contains the IP
	return cidrNet.Contains(ip)
}
