// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package httpboot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
)

const (
	optionDisabled = iota
	optionEnabled
	optionMultiple
	expectedGenericBootURL       = "https://[2001:db8::1]/boot.uki"
	expectedCustomBootURL        = "https://[2001:db8::1]/client-specific/boot.uki"
	expectedDefaultCustomBootURL = "https://[2001:db8::1]/default.uki"
	bootServiceEndpoint          = "bootservice:http://[::1]:%d/httpboot"
	expectedEnterpriseNumber     = 0
	bootServicePort              = 8888
)

var (
	expectedHTTPClient = []byte("HTTPClient")
)

func Init4(bootURL string) {
	_, err := setup4(bootURL)
	if err != nil {
		log.Fatal(err)
	}
}

func Init6(bootURL string) {
	_, err := setup6(bootURL)
	if err != nil {
		log.Fatal(err)
	}
}

/* parametrization */

func TestWrongNumberArgs(t *testing.T) {
	_, _, err := parseArgs("foo", "bar")
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (2), but it should have")
	}

	_, _, err = parseArgs()
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (0), but it should have")
	}
}

func TestWrongArgs(t *testing.T) {
	malformedBootURL := []string{"ftp://www.example.com/boot.uki",
		"tftp:/www.example.com/boot.uki",
		"foobar:/www.example.com/boot.uki",
		"bootfail:https://www.example.com/boot.uki",
		"bootservice:tftp://www.example.com/boot.uki"}

	for _, wrongURL := range malformedBootURL {
		_, err := setup4(wrongURL)
		if err == nil {
			t.Fatalf("no error occurred when parsing wrong boot param %s, but it should have", wrongURL)
		}
		_, err = setup6(wrongURL)
		if err == nil {
			t.Fatalf("no error occurred when parsing wrong boot param %s, but it should have", wrongURL)
		}
	}
}

/* IPv6 */
func TestGenericHTTPBootRequested6(t *testing.T) {
	Init6(expectedGenericBootURL)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optVendorClass := dhcpv6.OptVendorClass{}
	buf := []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 10, // length ot vendor class
		'H', 'T', 'T', 'P', 'C', 'l', 'i', 'e', 'n', 't', // vendor class
	}
	_ = optVendorClass.FromBytes(buf)
	req.UpdateOption(&optVendorClass)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionEnabled, len(opts), opts)
	}

	bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
	if bootFileURL != expectedGenericBootURL {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, expectedGenericBootURL)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionEnabled, len(opts), opts)
	}

	vc := resp.(*dhcpv6.Message).Options.VendorClasses()[0]
	if vc.EnterpriseNumber != expectedEnterpriseNumber {
		t.Errorf("Found EnterpriseNumber %d, expected %d", vc.EnterpriseNumber, expectedEnterpriseNumber)
	}

	vcData := resp.(*dhcpv6.Message).Options.VendorClass(vc.EnterpriseNumber)
	if !bytes.Equal(vcData[0], expectedHTTPClient) {
		t.Errorf("Found VendorClass %x, expected %x", vcData[0], expectedHTTPClient)
	}
}

func TestMalformedHTTPBootRequested6(t *testing.T) {
	Init6(expectedGenericBootURL)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optVendorClass := dhcpv6.OptVendorClass{}
	buf := []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 5, // WRONG LENGTH!
		'H', 'T', 'T', 'P', 'C', 'l', 'i', 'e', 'n', 't', // vendor class
	}
	_ = optVendorClass.FromBytes(buf)
	req.UpdateOption(&optVendorClass)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionDisabled, len(opts), opts)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionDisabled, len(opts), opts)
	}

	buf = []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 10, // length ot vendor class
		'H', 'T', 'T', 'P', 'F', 'O', 'O', 'B', 'A', 'R', // WRONG VC
	}
	_ = optVendorClass.FromBytes(buf)
	req.UpdateOption(&optVendorClass)

	resp, stop = handler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts = resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionDisabled, len(opts), opts)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionDisabled, len(opts), opts)
	}
}

func TestHTTPBootNotRequested6(t *testing.T) {
	Init6(expectedGenericBootURL)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	// known LinkAddr
	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, net.IPv6loopback)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(relayedRequest, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionDisabled, len(opts), opts)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionDisabled, len(opts), opts)
	}
}

func TestHTTPBootNotRelayedMsg6(t *testing.T) {
	Init6(expectedGenericBootURL)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionDisabled, len(opts), opts)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionDisabled, len(opts), opts)
	}
}

/* IPv4 */
func TestGenericHTTPBootRequested4(t *testing.T) {
	Init4(expectedGenericBootURL)

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionClassIdentifier),
	)
	if err != nil {
		t.Fatal(err)
	}

	optClassID := dhcpv4.OptClassIdentifier("HTTPClient")
	req.UpdateOption(optClassID)

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := handler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	bootFileName := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileName != expectedGenericBootURL {
		t.Errorf("Found BootFileName %s, expected %s", bootFileName, expectedGenericBootURL)
	}

	ci := dhcpv4.GetString(dhcpv4.OptionClassIdentifier, resp.Options)
	if ci != string(expectedHTTPClient) {
		t.Errorf("Found ClassIdentifier %s, expected %s", ci, string(expectedHTTPClient))
	}
}

func TestMalformedHTTPBootRequested4(t *testing.T) {
	Init4(expectedGenericBootURL)

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionClassIdentifier),
	)
	if err != nil {
		t.Fatal(err)
	}

	optClassID := dhcpv4.OptClassIdentifier("HTTPC")
	req.UpdateOption(optClassID)

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := handler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	emptyBootFileName := ""
	bootFileName := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileName != emptyBootFileName {
		t.Errorf("Found BootFileName %s, expected %s", bootFileName, emptyBootFileName)
	}

	emptyClassIdentifier := ""
	ci := dhcpv4.GetString(dhcpv4.OptionClassIdentifier, resp.Options)
	if ci != emptyClassIdentifier {
		t.Errorf("Found ClassIdentifier %s, expected %s", ci, emptyClassIdentifier)
	}

	optClassID = dhcpv4.OptClassIdentifier("HTTPFOOBAR")
	req.UpdateOption(optClassID)

	stub, err = dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop = handler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	emptyBootFileName = ""
	bootFileName = dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileName != emptyBootFileName {
		t.Errorf("Found BootFileName %s, expected %s", bootFileName, emptyBootFileName)
	}

	emptyClassIdentifier = ""
	ci = dhcpv4.GetString(dhcpv4.OptionClassIdentifier, resp.Options)
	if ci != emptyClassIdentifier {
		t.Errorf("Found ClassIdentifier %s, expected %s", ci, emptyClassIdentifier)
	}
}

func TestHTTPBootNotRequested4(t *testing.T) {
	Init4(expectedGenericBootURL)

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionClassIdentifier),
	)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := handler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	emptyBootFileName := ""
	bootFileName := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileName != emptyBootFileName {
		t.Errorf("Found BootFileName %s, expected %s", bootFileName, emptyBootFileName)
	}

	emptyClassIdentifier := ""
	ci := dhcpv4.GetString(dhcpv4.OptionClassIdentifier, resp.Options)
	if ci != emptyClassIdentifier {
		t.Errorf("Found ClassIdentifier %s, expected %s", ci, emptyClassIdentifier)
	}
}

/* client-specific tests */
func TestCustomHTTPBootRequestedKnownIP(t *testing.T) {
	go startBootServiceMock()
	time.Sleep(time.Second * 1)

	Init6(fmt.Sprintf(bootServiceEndpoint, bootServicePort))
	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optVendorClass := dhcpv6.OptVendorClass{}
	buf := []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 10, // length ot vendor class
		'H', 'T', 'T', 'P', 'C', 'l', 'i', 'e', 'n', 't', // vendor class
	}
	_ = optVendorClass.FromBytes(buf)
	req.UpdateOption(&optVendorClass)

	// known LinkAddr
	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, net.IPv6loopback)
	if err != nil {
		t.Fatal(err)
	}

	// not known LinkLayerAddress
	macAddress, _ := net.ParseMAC("11:22:33:44:55:66")
	opt := dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, macAddress)
	relayedRequest.AddOption(opt)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(relayedRequest, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionEnabled, len(opts), opts)
	}

	bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
	if bootFileURL != expectedCustomBootURL {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, expectedCustomBootURL)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionEnabled, len(opts), opts)
	}

	vc := resp.(*dhcpv6.Message).Options.VendorClasses()[0]
	if vc.EnterpriseNumber != expectedEnterpriseNumber {
		t.Errorf("Found EnterpriseNumber %d, expected %d", vc.EnterpriseNumber, expectedEnterpriseNumber)
	}

	vcData := resp.(*dhcpv6.Message).Options.VendorClass(vc.EnterpriseNumber)
	if !bytes.Equal(vcData[0], expectedHTTPClient) {
		t.Errorf("Found VendorClass %x, expected %x", vcData[0], expectedHTTPClient)
	}
}

func TestCustomHTTPBootRequestedKnownMAC(t *testing.T) {
	// known LinkLayerAddress
	macAddress, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")

	err, relayedRequest := createHTTPBootRequest(t)
	if err != nil {
		t.Fatal(err)
	}

	ensureBootURL(t, macAddress, relayedRequest, expectedCustomBootURL)
}

func ensureBootURL(t *testing.T, macAddress net.HardwareAddr, relayedRequest *dhcpv6.RelayMessage, expectedBootURL string) {
	opt := dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, macAddress)
	relayedRequest.AddOption(opt)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(relayedRequest, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionEnabled, len(opts), opts)
	}

	bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
	if bootFileURL != expectedBootURL {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, expectedBootURL)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionEnabled, len(opts), opts)
	}

	vc := resp.(*dhcpv6.Message).Options.VendorClasses()[0]
	if vc.EnterpriseNumber != expectedEnterpriseNumber {
		t.Errorf("Found EnterpriseNumber %d, expected %d", vc.EnterpriseNumber, expectedEnterpriseNumber)
	}

	vcData := resp.(*dhcpv6.Message).Options.VendorClass(vc.EnterpriseNumber)
	if !bytes.Equal(vcData[0], expectedHTTPClient) {
		t.Errorf("Found VendorClass %x, expected %x", vcData[0], expectedHTTPClient)
	}
}

func TestCustomHTTPBootRequestedUnknownClient(t *testing.T) {
	// not known LinkLayerAddress
	macAddress, _ := net.ParseMAC("11:22:33:44:55:66")

	err, relayedRequest := createHTTPBootRequest(t)
	if err != nil {
		t.Fatal(err)
	}

	ensureBootURL(t, macAddress, relayedRequest, expectedDefaultCustomBootURL)
}

func createHTTPBootRequest(t *testing.T) (error, *dhcpv6.RelayMessage) {
	Init6(fmt.Sprintf(bootServiceEndpoint, bootServicePort))
	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optVendorClass := dhcpv6.OptVendorClass{}
	buf := []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 10, // length ot vendor class
		'H', 'T', 'T', 'P', 'C', 'l', 'i', 'e', 'n', 't', // vendor class
	}
	_ = optVendorClass.FromBytes(buf)
	req.UpdateOption(&optVendorClass)

	// not known LinkAddr
	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}, net.IPv6loopback)
	if err != nil {
		t.Fatal(err)
	}
	return err, relayedRequest
}

func TestNoRelayCustomHTTPBootRequested(t *testing.T) {
	Init6(fmt.Sprintf(bootServiceEndpoint, bootServicePort))

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optVendorClass := dhcpv6.OptVendorClass{}
	buf := []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 10, // length ot vendor class
		'H', 'T', 'T', 'P', 'C', 'l', 'i', 'e', 'n', 't', // vendor class
	}
	_ = optVendorClass.FromBytes(buf)
	req.UpdateOption(&optVendorClass)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", optionDisabled, len(opts), opts)
	}

	opts = resp.GetOption(dhcpv6.OptionVendorClass)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d VendorClass option, got %d: %v", optionDisabled, len(opts), opts)
	}
}

func startBootServiceMock() {
	// Set up a simple HTTP server
	http.HandleFunc("/httpboot", httpHandler)

	fmt.Printf("Starting server at port %d", bootServicePort)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", bootServicePort), nil); err != nil {
		panic("All ports are already in use")
	}
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
	// Get the X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")

	clientIPs := strings.Split(xff, ", ")
	httpBootResponseData := make(map[string]string)

	goon := true
	for _, clientIP := range clientIPs {
		if !goon {
			break
		}

		switch clientIP {
		case "::1":
			log.Printf("Match for client IP '%s' found", clientIP)
			httpBootResponseData["ClientIPs"] = strings.Join(clientIPs, ", ")
			httpBootResponseData["UKIURL"] = expectedCustomBootURL
			goon = false
		case "aa:bb:cc:dd:ee:ff":
			log.Printf("Match for client MAC '%s' found", clientIP)
			httpBootResponseData["ClientIPs"] = strings.Join(clientIPs, ", ")
			httpBootResponseData["UKIURL"] = expectedCustomBootURL
			goon = false
		default:
			log.Printf("Client IP/MAC '%s' does not match", clientIP)
		}
	}

	if len(httpBootResponseData) == 0 {
		log.Printf("Delivering default UKI image")
		httpBootResponseData["ClientIPs"] = strings.Join(clientIPs, ", ")
		httpBootResponseData["UKIURL"] = expectedDefaultCustomBootURL
	}

	// Generate response based on the X-Forwarded-For header
	response, err := json.Marshal(httpBootResponseData)
	if err != nil {
		log.Error(err, "Failed to marshal response data")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(response); err != nil {
		log.Error(err, "Failed to write response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
