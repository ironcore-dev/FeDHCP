// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/ironcore-dev/fedhcp/internal/helper"

	"k8s.io/apimachinery/pkg/types"

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
	SubnetNames   []string
	EventRecorder record.EventRecorder
}

func NewK8sClient(namespace string, subnetNames []string) (*K8sClient, error) {
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

	k8sClient := K8sClient{
		Client:        cl,
		Namespace:     namespace,
		SubnetNames:   subnetNames,
		EventRecorder: recorder,
	}
	return &k8sClient, nil
}

func (k K8sClient) createIpamIP(ctx context.Context, ipaddr net.IP, mac net.HardwareAddr) error {
	var ipamIP *ipamv1alpha1.IP
	macKey := strings.ReplaceAll(mac.String(), ":", "")

	// select the subnet matching the CIDR of the request
	subnetMatch := false
	for _, subnetName := range k.SubnetNames {
		subnet, err := k.getMatchingSubnet(ctx, subnetName, ipaddr)
		if err != nil {
			log.Warningf("Error getting subnet %s/%s: %v", k.Namespace, subnetName, err)
			continue
		}
		if subnet == nil {
			continue
		}
		log.Debugf("Selecting subnet %s", client.ObjectKeyFromObject(subnet))
		subnetMatch = true

		ipamIP, err = k.prepareCreateIpamIP(ctx, subnetName, macKey, ipaddr)
		if err != nil {
			return err
		}
		if ipamIP == nil {
			if err = k.doCreateIpamIP(ctx, subnetName, macKey, ipaddr); err != nil {
				return err
			}
		} else {
			log.Debugf("Reserved IP %s (%s) already exists in subnet %s", ipamIP.Status.Reserved.String(),
				client.ObjectKeyFromObject(ipamIP), ipamIP.Spec.Subnet.Name)
		}
		// break at first subnet match, there can be only one
		break
	}

	if !subnetMatch {
		return fmt.Errorf("no matching subnet found for IP %s/%s", k.Namespace, ipaddr)
	}

	return nil
}

func (k K8sClient) prepareCreateIpamIP(ctx context.Context, subnetName string, macKey string, ipaddr net.IP) (*ipamv1alpha1.IP, error) {
	if _, err := ipamv1alpha1.IPAddrFromString(ipaddr.String()); err != nil {
		return nil, fmt.Errorf("failed to parse IP %s: %w", ipaddr, err)
	}

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
			log.Debugf("Deleting old IP %s:\n%v", client.ObjectKeyFromObject(&existingIpamIP), helper.PrettyFormat(existingIpamIP.Status, log))
			if err := k.Client.Delete(ctx, &existingIpamIP); err != nil {
				return nil, fmt.Errorf("failed to delete IP %s: %w", client.ObjectKeyFromObject(&existingIpamIP), err)
			}

			if err := helper.WaitForIPDeletion(ctx, &existingIpamIP); err != nil {
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

func (k K8sClient) doCreateIpamIP(ctx context.Context, subnetName string, macKey string, ipaddr net.IP) error {
	parsedIP, err := ipamv1alpha1.IPAddrFromString(ipaddr.String())
	if err != nil {
		return fmt.Errorf("failed to parse IP %s: %w", ipaddr, err)
	}

	ipamIP := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: macKey + "-" + origin + "-",
			Namespace:    k.Namespace,
			Labels: map[string]string{
				"mac":    macKey,
				"origin": origin,
			},
		},
		Spec: ipamv1alpha1.IPSpec{
			IP: parsedIP,
			Subnet: corev1.LocalObjectReference{
				Name: subnetName,
			},
		},
	}

	log.Infof("Creating new IP for MAC address %s", macKey)
	if err = k.Client.Create(ctx, ipamIP); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
		} else {
			// do not create IP, because the deletion is not yet ready
			return nil
		}
	}

	ipamIP, err = helper.WaitForIPCreation(ctx, ipamIP)
	if err != nil {
		return fmt.Errorf("failed to create IP %w", err)
	} else {
		log.Infof("New IP %s (%s/%s) created in subnet %s", ipamIP.Status.Reserved.String(),
			ipamIP.Namespace, ipamIP.Name, ipamIP.Spec.Subnet.Name)
		k.EventRecorder.Eventf(ipamIP, corev1.EventTypeNormal, "Created", "Created IPAM IP")

		// update IP attributes
		createdIpamIP := ipamIP.DeepCopy()
		err := k.Client.Get(ctx, client.ObjectKeyFromObject(createdIpamIP), createdIpamIP)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get IP %s: %w", client.ObjectKeyFromObject(createdIpamIP), err)
		}
		return nil
	}
}

func (k K8sClient) getMatchingSubnet(ctx context.Context, subnetName string, ipaddr net.IP) (*ipamv1alpha1.Subnet, error) {
	subnet := &ipamv1alpha1.Subnet{}
	if err := k.Client.Get(ctx, types.NamespacedName{Name: subnetName, Namespace: k.Namespace}, subnet); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get subnet %s: %w", client.ObjectKeyFromObject(subnet), err)
		} else {
			return nil, fmt.Errorf("cannot select subnet %s, does not exist", client.ObjectKeyFromObject(subnet))
		}
	}

	if !helper.CheckIPInCIDR(ipaddr, subnet.Status.Reserved.String(), log) {
		log.Debugf("Cannot select subnet %s, CIDR mismatch", client.ObjectKeyFromObject(subnet))
		return nil, nil
	}

	return subnet, nil
}
