[![REUSE status](https://api.reuse.software/badge/github.com/ironcore-dev/FeDHCP)](https://api.reuse.software/info/github.com/ironcore-dev/FeDHCP)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat-square)](http://makeapullrequest.com)
[![GitHub License](https://img.shields.io/static/v1?label=License&message=Apache-2.0&color=blue&style=flat-square)](LICENSE)
[![Build and Publish Docker Image](https://github.com/ironcore-dev/FeDHCP/actions/workflows/publish-docker.yml/badge.svg)](https://github.com/ironcore-dev/FeDHCP/actions/workflows/publish-docker.yml)

# Description
`FeDHCP` is a DHCP server for the [IronCore Project](https://github.com/ironcore-dev) network. It is based on [coredhcp](https://github.com/coredhcp/coredhcp).


# Plugins
## Bluefield
Leases a single IP address to a single client as a [non temporary IPv6 address](https://datatracker.ietf.org/doc/html/rfc8415#section-6.2).

Meant to be used in 1:1 client-server connection scenarios, for example a smartnic (Bluefield) leasing a single address to the host.
### Configuration
The IP address to lease shall be passed as a string in `bulefield_config.yaml` goes as follows:

```yaml
bulefieldIP: 2001:db8::1
```


### Notes
- supports IPv6 addresses only
- IPv6 relays are supported

## HTTPBoot
Implements HTTP boot from [Unifed Kernel Image](https://uapi-group.org/specifications/specs/unified_kernel_image/).

Delivers the HTTP boot image as a [BootFileURL](https://www.rfc-editor.org/rfc/rfc5970.html#section-3.2). Based on configuration it delivers either a client-specific UKIs dynamically or a default UKI for all clients. When client-specific UKIs are configured, IPv6 relays *must* be used, so the client can be identified based on its link-local address (which the relay always provides).

### Configuration
A single HTTP(s) URL shall be passed as a string in `httpboot_config.yaml` as given below. It must be either
- a direct URL to an UKI (default UKI for all clients)
- magic identifier `bootservice:`+ a URL to a boot service delivering dynamically client-specific UKIs based on client identification
````yaml
bootServer: http://[2001:db8::1]/image.uki
````
### Notes
- not tested on IPv4
- IPv6 relays are supported
- the only supported client-specific UKI delivery service is the [IronCore Boot Operator](https://github.com/ironcore-dev/boot-operator/)
- only EFI X64_64 architecture is supported, see https://github.com/ironcore-dev/FeDHCP/issues/154

## IPAM
The IPAM plugin acts as a Kubernetes persistence plugin for IronCore's in-band network. Thus, it's meant to be used in combination with the `onmetal` plugin only. Those two may be consolidated in the future into a new plugin called `inband`.

The IPAM plugin does not modify DHCP responses to the client, it rather creates (or updates) IP objects in Kubernetes. For each created IP object, the in-band plugin `onmetal` will lease an IP address to the client. Due to the nature of the IronCore's in-band network - `/127` client networks connected to each switch port - the IP object created has and address calculated by a simple "plus one" rule. In such a way each client gets a "plus one" of the switch port address it is connected to.
###  Configuration
The IPAM configuration consists of two parameters. First, a kubernetes namespace shall be defined. All IPAM processing (subnet identification, IP object creation/update) are done in that namespace.
Further, a list of subnet names shall be passed. The IPAM plugin will do the subnet creation based on the IP address of the object to be created, as well as on the vacant range of the corresponding subnet.
Providing those in `ipam_config.yaml` goes as follows:
```yaml
namespace: ipam-ns
subnets:
  - ipam-subnet1
  - ipam-subnet2
  - some-other-subnet
```
### Notes
- supports only IPv6
- IPv6 relays are mandatory
- shall be used in combination with `onmetal` plugin
- IP addresses are just created/updated, they are not deleted upon DHCP IP address release. Cleanup process is still tbd.
- depends on [IPAM operator](https://github.com/ironcore-dev/ipam)

## OnMetal
The OnMetal plugin leases a [non temporary IPv6 address](https://datatracker.ietf.org/doc/html/rfc8415#section-6.2) to an in-band client, based on the algorithm described above. Additionally, when requested from the client, a prefix delegation with preconfigured length is leased. Currently multiple prefix delegations are not supported, client prefix delegation length proposals are ignored completely. The prefix delegation length should be in the range 1 <= length <= 127.
### Configuration
The onmetal configuration consists of the prefix delegation length only.
Providing the length in `onmetal_config.yaml` goes as follows:
```yaml
prefixDelegation:
  length: 64
```
### Notes
- supports only IPv6
- IPv6 relays are mandatory
- can be used standalone or (in combination with the `ipam` plugin) in kubernetes

## OOB
The OOB plugin leases an IP address to an out-of-band client, based on a subnet detection. For each a corresponding IPAM IP object is created. Thus, the plugin is an equivalent of the onmetal+ipam combination, though it is meant to be used in IronCore's out-of-band network.

An IP object with a random IP address from the subnet's vacant list is created in IPAM, the IP address is then leased back to the client. Currently, no cleanup-on-release is performed, so clients with stable identifiers are guaranteed to become stable IP addresses.
### Configuration
As for in-band, a kubernetes namespace shall be passed as a parameter. Further, a subnet label list in the form `value:key` shall be passed, it is used for subnet detection.
Providing those in `oob_config.yaml` goes as follows:
```yaml
namespace: oob-ns
subnetLabel: subnet=dhcp
```
### Notes
- supports both IPv4 and IPv6
- IPv6 relays are supported, IPv4 are not
- other than for in-band, where the DHCP leasing and kubernetes persistence are handled in different plugins, for out-of-band a single plugin is used
- depends on [IPAM operator](https://github.com/ironcore-dev/ipam)
 
## Metal
The Metal plugin acts as a connection link between DHCP and the IronCore metal stack. It creates an `EndPoint` object for each machine with leased IP address. Those endpoints are then consumed by the metal operator, who then creates the corresponding `Machine` objects.

### Configuration
The metal configuration consists of an inventory list. Currently, there are two different ways to provide an inventory list: either by specifying a MAC address filter or by providing the inventory list explicitly. If both a static list and a filter are specified in the `metal_config.yaml`, the static list gets a precedence, so the filter will be ignored. Providing an explicit static inventory list in `metal_config.yaml` goes as follows:
```yaml
hosts:
  - name: server-01
    macAddress: 00:1A:2B:3C:4D:5E
  - name: server-02
    macAddress: 00:1A:2B:3C:4D:5F
```

Providing a MAC address prefix filter list creates `Endpoint`s with a predefined prefix name. When the MAC address of an inventory does not match the prefix, the inventory will not be onboarded, so for now no "onboarding by default" occurs. Obviously a full MAC address is a valid prefix filter.
To get inventories with certain MACs onboarded, the following `metal_config.yaml` shall be specified:
```yaml
namePrefix: server- # optional prefix, default: "compute-"
filter:
  macPrefix:
    - 00:1A:2B:3C:4D:5E
    - 00:1A:2B:3C:4D:5F
    - 00:AA:BB
```
The inventories above will get auto-generated names like `server-aybz`.

### Notes
- supports both IPv4 and IPv6
- IPv6 relays are supported, IPv4 are not
- depends on [metal operator](https://github.com/ironcore-dev/metal)

## PXEBoot
The PXEBoot plugin implements an (i)PXE network boot.

When configured properly, the PXEBoot plugin will [break the PXE chainloading loop](https://ipxe.org/howto/dhcpd#pxe_chainloading). In such a way legacy PXE clients will be handed out an iPXE environment, whereas iPXE clients (classified based on the user class for [IPv6](https://datatracker.ietf.org/doc/html/rfc8415#section-21.15) and [IPv4](https://www.rfc-editor.org/rfc/rfc3004.html#section-4)) will get the HTTP PXE boot script.
### Configuration
A TFTP address to an iPXE environment and an HTTP(s) boot script address shall be specified. Providing those in the `pxeboot_config.yaml` goes as follows:

````yaml
tftpServer: tftp://[2001:db8::1]/ipxe/x86_64/ipxe
ipxeServer: http://[2001:db8::1]/ipxe/boot6
````

### Notes
- relays are supported for both IPv4 and IPv6
- TFTP server as well as HTTP boot script server must be provided externally
- as with `HTTPBoot`. only EFI X64_64 architecture is supported

# License
`FeDHCP` is licensed under [MIT License](LICENSE) - Copyright 2018-2024 by *coredhcp* and the *FeDHCP* authors.
