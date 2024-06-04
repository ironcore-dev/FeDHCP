// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/watch"

	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	ipam "github.com/ironcore-dev/ipam/clientgo/ipam"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	origin = "fedhcp"
)

type K8sClient struct {
	Client        client.Client
	Clientset     ipam.Clientset
	Namespace     string
	OobLabel      string
	Ctx           context.Context
	EventRecorder record.EventRecorder
}

func NewK8sClient(namespace string, oobLabel string) (*K8sClient, error) {

	if err := ipamv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, fmt.Errorf("unable to add registered types ipam to client scheme %w", err)
	}

	cfg := config.GetConfigOrDie()
	cl, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller runtime client %w", err)
	}

	clientset, err := ipam.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPAM clientset %w", err)
	}

	corev1Client, err := corev1client.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create core client %w", err)
	}

	broadcaster := record.NewBroadcaster()

	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname %w", err)
	}
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: id})
	broadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: corev1Client.Events("")})

	k8sClient := K8sClient{
		Client:        cl,
		Clientset:     *clientset,
		Namespace:     namespace,
		OobLabel:      oobLabel,
		Ctx:           context.Background(),
		EventRecorder: recorder,
	}

	return &k8sClient, nil
}

func (k K8sClient) getIp(ipaddr net.IP, mac net.HardwareAddr, exactIP bool, subnetType ipamv1alpha1.SubnetAddressType) (net.IP, error) {
	var ipamIP *ipamv1alpha1.IP
	macKey := strings.ReplaceAll(mac.String(), ":", "")

	subnetNames := k.getOOBNetworks(subnetType)
	if len(subnetNames) == 0 {
		return nil, errors.New("No OOB subnets found")
	} else {
		log.Debugf("%d OOB subnets found: %s", len(subnetNames), strings.Join(subnetNames, " "))
		subnetMatch := false
		for _, subnetName := range subnetNames {
			subnet, err := k.getMatchingSubnet(subnetName, ipaddr)
			if err != nil {
				return nil, err
			}
			if subnet == nil {
				continue
			}
			log.Debugf("Selecting subnet %s/%s", k.Namespace, subnetName)
			subnetMatch = true

			ipamIP, err = k.prepareCreateIpamIP(subnetName, macKey)
			if err != nil {
				return nil, err
			}
			if ipamIP == nil {
				ipamIP, err = k.doCreateIpamIP(subnetName, macKey, ipaddr, exactIP)
				if err != nil {
					return nil, err
				}
			} else {
				log.Infof("Reserved IP %s (%s/%s) already exists in subnet %s", ipamIP.Status.Reserved.String(), ipamIP.Namespace, ipamIP.Name, ipamIP.Spec.Subnet.Name)
				k.applySubnetLabel(ipamIP)
			}
			// break at first subnet match, there can be only one
			break
		}
		if !subnetMatch {
			return nil, errors.New(fmt.Sprintf("No matching subnet found for IP %s/%s", k.Namespace, ipaddr))
		}
	}

	if ipamIP.Status.Reserved != nil {
		return net.ParseIP(ipamIP.Status.Reserved.String()), nil
	} else {
		return nil, errors.New("No reserved IP address found")
	}
}

func (k K8sClient) prepareCreateIpamIP(subnetName string, macKey string) (*ipamv1alpha1.IP, error) {
	namespace := k.Namespace
	fieldSelector := "metadata.namespace=" + namespace
	// https://github.com/ironcore-dev/ipam/issues/307
	// fieldSelector += ",spec.subnet.name=" + subnetName
	labelSelector := "mac=" + macKey
	//labelSelector += ",origin=" + origin
	timeout := int64(5)

	ipList, err := k.Clientset.IpamV1alpha1().IPs(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector:  fieldSelector,
		LabelSelector:  labelSelector,
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("error listing IPs with MAC %v: %w", macKey, err)
	}
	if len(ipList.Items) == 0 {
		noop()
	} else {
		for _, existingIpamIP := range ipList.Items {
			if existingIpamIP.Spec.Subnet.Name != subnetName {
				// IP with that MAC is assigned to a different subnet (v4 vs v6?)
				log.Debugf("IPAM IP with MAC %v and wrong subnet %s/%s found, ignoring", macKey, existingIpamIP.Namespace, existingIpamIP.Spec.Subnet.Name)
				continue
			} else if existingIpamIP.Status.State == ipamv1alpha1.CFailedIPState {
				log.Infof("Failed IP %s/%s in subnet %s found, deleting", existingIpamIP.Namespace, existingIpamIP.Name, existingIpamIP.Spec.Subnet.Name)
				log.Debugf("Deleting old IP %s/%s:\n%v", existingIpamIP.Namespace, existingIpamIP.Name, prettyFormat(existingIpamIP.Status))
				err = k.Client.Delete(k.Ctx, &existingIpamIP)
				if err != nil {
					return nil, fmt.Errorf("failed to delete IP %s/%s: %w", existingIpamIP.Namespace, existingIpamIP.Name, err)
				}

				err = k.waitForDeletion(&existingIpamIP)
				if err != nil {
					return nil, fmt.Errorf("failed to delete IP %s/%s: %w", existingIpamIP.Namespace, existingIpamIP.Name, err)
				}

				k.EventRecorder.Eventf(&existingIpamIP, corev1.EventTypeNormal, "Deleted", "Deleted old IPAM IP")
				log.Debugf("Old IP %s/%s deleted from subnet %s", existingIpamIP.Namespace, existingIpamIP.Name, existingIpamIP.Spec.Subnet.Name)
			} else {
				// IP already exists
				return &existingIpamIP, nil
			}
		}
	}

	return nil, nil
}

func (k K8sClient) doCreateIpamIP(subnetName string, macKey string, ipaddr net.IP, exactIP bool) (*ipamv1alpha1.IP, error) {
	oobLabelKey := strings.Split(k.OobLabel, "=")[0]
	oobLabelValue := strings.Split(k.OobLabel, "=")[1]
	var ipamIP *ipamv1alpha1.IP
	if ipaddr.String() == UNKNOWN_IP || !exactIP {
		ipamIP = &ipamv1alpha1.IP{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: macKey + "-" + origin + "-",
				Namespace:    k.Namespace,
				Labels: map[string]string{
					"mac":       macKey,
					"origin":    origin,
					oobLabelKey: oobLabelValue,
				},
			},
			Spec: ipamv1alpha1.IPSpec{
				Subnet: corev1.LocalObjectReference{
					Name: subnetName,
				},
			},
		}
	} else {
		ip, _ := ipamv1alpha1.IPAddrFromString(ipaddr.String())
		ipamIP = &ipamv1alpha1.IP{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: macKey + "-" + origin + "-",
				Namespace:    k.Namespace,
				Labels: map[string]string{
					"mac":       macKey,
					"origin":    origin,
					oobLabelKey: oobLabelValue,
				},
			},
			Spec: ipamv1alpha1.IPSpec{
				IP: ip,
				Subnet: corev1.LocalObjectReference{
					Name: subnetName,
				},
			},
		}
	}

	err := k.Client.Create(k.Ctx, ipamIP)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create IP %s/%s: %w", ipamIP.Namespace, ipamIP.Name, err)
	} else if apierrors.IsAlreadyExists(err) {
		// do not create IP, because the deletion is not yet ready
		noop()
	} else {
		ipamIP, err = k.waitForCreation(ipamIP)
		if err != nil {
			return nil, fmt.Errorf("failed to create IP %s/%s: %w", ipamIP.Namespace, ipamIP.Name, err)
		} else {
			log.Infof("New IP %s (%s/%s) created in subnet %s", ipamIP.Status.Reserved.String(), ipamIP.Namespace, ipamIP.Name, ipamIP.Spec.Subnet.Name)
			k.EventRecorder.Eventf(ipamIP, corev1.EventTypeNormal, "Created", "Created IPAM IP")

			// update IP attributes
			createdIpamIP := ipamIP.DeepCopy()
			err := k.Client.Get(k.Ctx, client.ObjectKeyFromObject(createdIpamIP), createdIpamIP)
			if err != nil && !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("Failed to get IP %s/%s: %w", createdIpamIP.Namespace, createdIpamIP.Name, err)
			}
			return createdIpamIP, nil
		}
	}

	return nil, nil
}

func (k K8sClient) waitForDeletion(ipamIP *ipamv1alpha1.IP) error {
	// Define the namespace and resource name (if you want to watch a specific resource)
	namespace := ipamIP.Namespace
	resourceName := ipamIP.Name
	fieldSelector := "metadata.name=" + resourceName + ",metadata.namespace=" + namespace
	timeout := int64(5)

	// watch for deletion finished event
	watcher, err := k.Clientset.IpamV1alpha1().IPs(namespace).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector:  fieldSelector,
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		log.Errorf("Error watching for IP: %v", err)
	}

	log.Tracef("Watching for changes to IP %s/%s...", namespace, resourceName)

	for event := range watcher.ResultChan() {
		log.Tracef("Type: %s, Object: %v\n", event.Type, event.Object)
		existingIpamIP := event.Object.(*ipamv1alpha1.IP)
		if event.Type == watch.Deleted && reflect.DeepEqual(ipamIP.Spec, existingIpamIP.Spec) {
			log.Infof("IP %s/%s deleted", existingIpamIP.Namespace, existingIpamIP.Name)
			return nil
		}
	}
	return errors.New("Timeout reached, IP not deleted")
}

func (k K8sClient) waitForCreation(ipamIP *ipamv1alpha1.IP) (*ipamv1alpha1.IP, error) {
	// Define the namespace and resource name (if you want to watch a specific resource)
	namespace := ipamIP.Namespace
	resourceName := ipamIP.Name
	fieldSelector := "metadata.name=" + resourceName + ",metadata.namespace=" + namespace
	timeout := int64(10)

	// watch for creation finished event
	watcher, err := k.Clientset.IpamV1alpha1().IPs(namespace).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector:  fieldSelector,
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		log.Errorf("Error watching for IP: %v", err)
	}

	log.Tracef("Watching for changes to IP %s/%s...", namespace, resourceName)

	for event := range watcher.ResultChan() {
		log.Tracef("Type: %s, Object: %v\n", event.Type, event.Object)
		createdIpamIP := event.Object.(*ipamv1alpha1.IP)
		if event.Type == watch.Added || event.Type == watch.Modified {
			if createdIpamIP.Status.State == ipamv1alpha1.CFinishedIPState {
				log.Debug("IP creation finished")
				return createdIpamIP, nil
			} else if createdIpamIP.Status.State == ipamv1alpha1.CProcessingIPState {
				continue
			} else if createdIpamIP.Status.State == ipamv1alpha1.CFailedIPState {
				return nil, errors.New("Failed to create IP address")
			}
		}
	}
	return nil, errors.New("Timeout reached, IP not created")
}

func (k K8sClient) getOOBNetworks(subnetType ipamv1alpha1.SubnetAddressType) []string {
	timeout := int64(5)

	subnetList, err := k.Clientset.IpamV1alpha1().Subnets(k.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector:  k.OobLabel,
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		log.Errorf("Error listing OOB subnets: %v", err)
	}

	oobSubnetNames := []string{}
	for _, subnet := range subnetList.Items {
		if subnet.Status.Type == subnetType {
			oobSubnetNames = append(oobSubnetNames, subnet.Name)
		}
	}

	return oobSubnetNames
}

func (k K8sClient) getMatchingSubnet(subnetName string, ipaddr net.IP) (*ipamv1alpha1.Subnet, error) {
	subnet := &ipamv1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subnetName,
			Namespace: k.Namespace,
		},
	}
	existingSubnet := subnet.DeepCopy()
	err := k.Client.Get(k.Ctx, client.ObjectKeyFromObject(subnet), existingSubnet)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get subnet %s/%s: %w", k.Namespace, subnetName, err)
	}
	if apierrors.IsNotFound(err) {
		log.Debugf("Cannot select subnet %s/%s, does not exist", k.Namespace, subnetName)
		return nil, nil
	}
	if !checkIPInCIDR(ipaddr, existingSubnet.Status.Reserved.String()) && ipaddr.String() != UNKNOWN_IP {
		log.Debugf("Cannot select subnet %s/%s, CIDR mismatch", k.Namespace, subnetName)
		return nil, nil
	}

	return subnet, nil
}

func (k K8sClient) applySubnetLabel(ipamIP *ipamv1alpha1.IP) {
	oobLabelKey := strings.Split(k.OobLabel, "=")[0]
	oobLabelValue := strings.Split(k.OobLabel, "=")[1]

	log.Debugf("Current labels: %v", ipamIP.Labels)

	_, exists := ipamIP.Labels[oobLabelKey]
	if exists && ipamIP.Labels[oobLabelKey] == oobLabelValue {
		log.Debug("Subnet label up-to-date")
	} else {
		if !exists {
			ipamIP, err := k.Clientset.IpamV1alpha1().IPs(ipamIP.Namespace).Get(context.TODO(), ipamIP.Name, metav1.GetOptions{})
			if err != nil {
				log.Errorf("Error applying subnet label to IPAM IP %s: %v\n", ipamIP.Name, err)
			} else {
				if ipamIP.Labels == nil {
					ipamIP.Labels = make(map[string]string)
				}
			}
		}

		ipamIP.Labels[oobLabelKey] = oobLabelValue
		_, err := k.Clientset.IpamV1alpha1().IPs(ipamIP.Namespace).Update(context.TODO(), ipamIP, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf("Error applying label to IPAM IP %s: %v\n", ipamIP.Name, err)
		} else {
			log.Debugf("Subnet label applied to IPAM IP %s\n", ipamIP.Name)
		}
	}
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

func noop() {}
