// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package httpboot

import (
	"bytes"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv6"
)

const (
	optionDisabled = iota
	optionEnabled
	optionMultiple
	expectedBootGenericURL   = "https://[2001:db8::1]/boot.uki"
	expectedBootCustomURL    = "bootservice:https://[2001:db8::1]/boot.uki"
	expectedEnterpriseNumber = 0
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
func TestGenericHTTPBootRequested(t *testing.T) {
	Init6(expectedBootGenericURL)

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
	if bootFileURL != expectedBootGenericURL {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, expectedBootGenericURL)
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

func TestMalformedHTTPBootRequested(t *testing.T) {
	Init6(expectedBootGenericURL)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optVendorClass := dhcpv6.OptVendorClass{}
	buf := []byte{
		0, 0, 5, 57, // nice "random" enterprise number, can be ignored
		0, 5, // WRONG LENGHT!
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

func TestHTTPBootNotRequested(t *testing.T) {
	Init6(expectedBootGenericURL)

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
