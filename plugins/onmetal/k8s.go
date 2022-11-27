package onmetal

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"reflect"
	"strings"

	ipamv1alpha1 "github.com/onmetal/ipam/api/v1alpha1"
	inventoryv1alpha1 "github.com/onmetal/metal-api/apis/inventory/v1alpha1"
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
	origin = "dhcp"
)

type K8sClient struct {
	Client        client.Client
	Namespace     string
	Subnet        string
	EventRecorder record.EventRecorder
}

func NewK8sClient(namespace string, subnet string) K8sClient {
	if err := inventoryv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatal("Unable to add registered types inventory to client scheme: ", err)
	}

	if err := ipamv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatal("Unable to add registered types inventory to client scheme: ", err)
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
		Subnet:        subnet,
		EventRecorder: recorder,
	}
}

func (k K8sClient) createIpamIP(ipaddr net.IP, mac net.HardwareAddr) error {
	ctx := context.Background()
	macKey := strings.ReplaceAll(mac.String(), ":", "")

	ip, err := ipamv1alpha1.IPAddrFromString(ipaddr.String())
	if err != nil {
		err = errors.Wrapf(err, "Failed to parse IP %s", ip)
		return err
	}

	// a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and
	// must start and end with an alphanumeric character.
	// 2001:abcd:abcd::1 will become 2001-abcd-abcd-0000-0000-0000-0000-00001
	longIpv6 := getLongIPv6(ipaddr)
	name := longIpv6 + "-" + origin
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
				Name: k.Subnet,
			},
		},
	}

	existingIpamIP := ipamIP.DeepCopy()
	err = k.Client.Get(ctx, client.ObjectKeyFromObject(ipamIP), existingIpamIP)
	if err != nil && !apierrors.IsNotFound(err) {
		err = errors.Wrapf(err, "Failed to get IP %s in namespace %s", ipamIP.Name, ipamIP.Namespace)
		return err
	}

	createIpamIP := false
	// create IPAM IP if not exists or delete existing if ip differs
	if apierrors.IsNotFound(err) {
		createIpamIP = true
	} else {
		if !reflect.DeepEqual(ipamIP.Spec, existingIpamIP.Spec) {
			log.Infof("Delete old IP %s in namespace %s", existingIpamIP.Name, existingIpamIP.Namespace)

			// delete old IP object
			err = k.Client.Delete(ctx, existingIpamIP)
			if err != nil {
				err = errors.Wrapf(err, "Failed to delete IP %s in namespace %s", existingIpamIP.Name, existingIpamIP.Namespace)
				return err
			}

			k.EventRecorder.Eventf(existingIpamIP, corev1.EventTypeNormal, "Reason", "Deleted old IPAM IP")
			createIpamIP = true
		}
	}

	if createIpamIP {
		err = k.Client.Create(ctx, ipamIP)
		if err != nil {
			err = errors.Wrapf(err, "Failed to create IP %s in namespace %s", ipamIP.Name, ipamIP.Namespace)
			return err
		}

		k.EventRecorder.Eventf(ipamIP, corev1.EventTypeNormal, "Reason", "Created IPAM IP")
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
