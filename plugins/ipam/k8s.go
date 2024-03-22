// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"reflect"
	"strings"
	"time"

	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	Namespace     string
	SubnetNames   []string
	Ctx           context.Context
	EventRecorder record.EventRecorder
}

func NewK8sClient(namespace string, subnetNames []string) K8sClient {
	if err := ipamv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatal("Unable to add registered types ipam to client scheme: ", err)
	}

	cfg := config.GetConfigOrDie()
	cl, err := client.New(cfg, client.Options{})
	if err != nil {
		log.Fatal("Failed to create a controller runtime client: ", err)
	}

	corev1Client, err := corev1client.NewForConfig(cfg)
	if err != nil {
		log.Fatal("Failed to create a core client: ", err)
	}

	broadcaster := record.NewBroadcaster()

	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		log.Fatal("Failed to get hostname: ", err)
	}
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: id})
	broadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: corev1Client.Events("")})

	return K8sClient{
		Client:        cl,
		Namespace:     namespace,
		SubnetNames:   subnetNames,
		Ctx:           context.Background(),
		EventRecorder: recorder,
	}
}

func (k K8sClient) createIpamIP(ipaddr net.IP, mac net.HardwareAddr) error {
	ip, err := ipamv1alpha1.IPAddrFromString(ipaddr.String())
	if err != nil {
		err = errors.Wrapf(err, "Failed to parse IP %s", ip)
		return err
	}

	// select the subnet matching the CIDR of the request
	subnetMatch := false
	for _, subnetName := range k.SubnetNames {
		subnet := k.getMatchingSubnet(subnetName, ipaddr)
		if subnet == nil {
			continue
		}
		log.Debugf("Selecting subnet %s", subnetName)
		subnetMatch = true

		var ipamIP *ipamv1alpha1.IP
		ipamIP, err = k.prepareCreateIpamIP(subnetName, ipaddr, mac)
		if err != nil {
			return err
		}

		if ipamIP != nil {
			err = k.doCreateIpamIP(ipamIP, subnetName)
			if err != nil {
				return err
			}
		}
		break
	}

	if !subnetMatch {
		log.Warningf("No matching subnet found for IP %s/%s", k.Namespace, ip)
	}

	return nil
}

func (k K8sClient) getMatchingSubnet(subnetName string, ipaddr net.IP) *ipamv1alpha1.Subnet {
	subnet := &ipamv1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subnetName,
			Namespace: k.Namespace,
		},
	}
	existingSubnet := subnet.DeepCopy()
	err := k.Client.Get(k.Ctx, client.ObjectKeyFromObject(subnet), existingSubnet)
	if err != nil && !apierrors.IsNotFound(err) {
		err = errors.Wrapf(err, "Failed to get subnet %s/%s", k.Namespace, subnetName)
		return nil
	}
	if apierrors.IsNotFound(err) {
		log.Debugf("Cannot select subnet %s/%s, does not exist", k.Namespace, subnetName)
		return nil
	}
	if !checkIPv6InCIDR(ipaddr, existingSubnet.Status.Reserved.String()) {
		log.Debugf("Cannot select subnet %s/%s, CIDR mismatch", k.Namespace, subnetName)
		return nil
	}

	return subnet
}

func (k K8sClient) prepareCreateIpamIP(subnetName string, ipaddr net.IP, mac net.HardwareAddr) (*ipamv1alpha1.IP, error) {
	ip, err := ipamv1alpha1.IPAddrFromString(ipaddr.String())
	// a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and
	// must start and end with an alphanumeric character.
	// 2001:abcd:abcd::1 will become 2001-abcd-abcd-0000-0000-0000-0000-00001
	longIpv6 := getLongIPv6(ipaddr)
	name := longIpv6 + "-" + origin
	macKey := strings.ReplaceAll(mac.String(), ":", "")
	ipamIP := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: k.Namespace,
			Labels: map[string]string{
				"ip":     longIpv6,
				"mac":    macKey,
				"origin": origin,
			},
		},
		Spec: ipamv1alpha1.IPSpec{
			IP: ip,
			Subnet: corev1.LocalObjectReference{
				Name: subnetName,
			},
		},
	}

	existingIpamIP := ipamIP.DeepCopy()
	err = k.Client.Get(k.Ctx, client.ObjectKeyFromObject(ipamIP), existingIpamIP)
	if err != nil && !apierrors.IsNotFound(err) {
		err = errors.Wrapf(err, "Failed to get IP %s/%s", existingIpamIP.Namespace, existingIpamIP.Name)
		return nil, err
	}

	// create IPAM IP if not exists or delete existing if ip differs
	if apierrors.IsNotFound(err) {
		noop()
	} else {
		if !reflect.DeepEqual(ipamIP.Spec, existingIpamIP.Spec) {
			log.Debugf("IP mismatch:\nold IP: %v,\nnew IP: %v", prettyFormat(existingIpamIP.Spec), prettyFormat(ipamIP.Spec))
			log.Infof("Delete old IP %s/%s", existingIpamIP.Namespace, existingIpamIP.Name)

			// delete old IP object
			err = k.Client.Delete(k.Ctx, existingIpamIP)
			if err != nil {
				err = errors.Wrapf(err, "Failed to delete IP %s/%s", existingIpamIP.Namespace, existingIpamIP.Name)
				return nil, err
			}

			k.EventRecorder.Eventf(existingIpamIP, corev1.EventTypeNormal, "Deleted", "Deleted old IPAM IP")
			log.Infof("Old IP deleted from subnet %s/%s, sleeping for 5 seconds, so the finalizer can run", k.Namespace, subnetName)
			time.Sleep(5 * time.Second)
		} else {
			log.Infof("IP already exists in subnet %s/%s, nothing to do", k.Namespace, subnetName)
			return nil, nil
		}
	}

	return ipamIP, nil
}

func (k K8sClient) doCreateIpamIP(ipamIP *ipamv1alpha1.IP, subnetName string) error {
	err := k.Client.Create(k.Ctx, ipamIP)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		err = errors.Wrapf(err, "Failed to create IP %s/%s", ipamIP.Namespace, ipamIP.Name)
		return err
	}
	if apierrors.IsAlreadyExists(err) {
		// do not create IP, because the deletion is not yet ready
		noop()
	} else {
		log.Infof("New IP created in subnet %s/%s", k.Namespace, subnetName)
		k.EventRecorder.Eventf(ipamIP, corev1.EventTypeNormal, "Created", "Created IPAM IP")
	}

	return nil
}

func getLongIPv6(ip net.IP) string {
	dst := make([]byte, hex.EncodedLen(len(ip)))
	_ = hex.Encode(dst, ip)

	longIpv6 := string(dst[0:4]) + ":" +
		string(dst[4:8]) + ":" +
		string(dst[8:12]) + ":" +
		string(dst[12:16]) + ":" +
		string(dst[16:20]) + ":" +
		string(dst[20:24]) + ":" +
		string(dst[24:28]) + ":" +
		string(dst[28:])

	return strings.ReplaceAll(longIpv6, ":", "-")
}

func prettyFormat(ipSpec ipamv1alpha1.IPSpec) string {
	// Marshal the struct into a JSON string with pretty printing
	jsonBytes, err := json.MarshalIndent(ipSpec, "", "  ")
	if err != nil {
		log.Errorf("Error marshalling JSON: %v", err)
	}

	// Convert the JSON bytes to a string and print
	return string(jsonBytes)
}

func checkIPv6InCIDR(ip net.IP, cidrStr string) bool {
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
