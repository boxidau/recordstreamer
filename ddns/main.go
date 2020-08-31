package main

import (
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
)

var interfacePrefix = flag.String(
	"interface",
	lookupEnvOrString("INTERFACE", "wlan"),
	"Interface prefix match, will use first matching interface name",
)
var hostedZoneID = flag.String(
	"hostedZoneID",
	lookupEnvOrString("HOSTED_ZONE_ID", ""),
	"Hosted zone ID to create a DDNS record in",
)
var subdomain = flag.String(
	"subdomain",
	lookupEnvOrString("SUBDOMAIN", ""),
	"subdomain section <deviceName>.<subdomain>.<domain>",
)

func lookupEnvOrString(key string, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

func usage() {
	flag.PrintDefaults()
	os.Exit(2)
}

func init() {
	flag.Usage = usage
	flag.Set("logtostderr", "true")
	flag.Parse()
}

type deviceInfo struct {
	name string
	IPv4 string
	IPv6 string
}

func main() {
	deviceInfo := deviceInfo{}
	for {
		curDeviceInfo := getDeviceInfo()
		if curDeviceInfo != deviceInfo {
			deviceInfo = curDeviceInfo
			createRecord(deviceInfo)
		} else {
			log.Println("No changes, not updating record")
		}
		time.Sleep(30 * time.Second)
	}
}

func createRecord(devInfo deviceInfo) {
	sesh := session.Must(session.NewSession())
	r53 := route53.New(sesh)

	zoneParams := &route53.GetHostedZoneInput{
		Id: aws.String(*hostedZoneID),
	}
	zoneResp, err := r53.GetHostedZone(zoneParams)
	if err != nil {
		log.Fatal(err)
	}
	domainName := zoneResp.HostedZone.Name

	subdomainStr := ""
	if *subdomain != "" {
		subdomainStr = *subdomain + "."
	}

	recordName := strings.ToLower(devInfo.name + "." + subdomainStr + *domainName)

	changes := make([]*route53.Change, 0, 2)
	changes = append(changes, &route53.Change{
		Action: aws.String("UPSERT"),
		ResourceRecordSet: &route53.ResourceRecordSet{
			Name: aws.String(recordName),
			Type: aws.String("A"),
			ResourceRecords: []*route53.ResourceRecord{
				{
					Value: aws.String(devInfo.IPv4),
				},
			},
			TTL: aws.Int64(60),
		},
	})

	log.Printf("Creating DDNS A record %v with value %v", recordName, devInfo.IPv4)

	if devInfo.IPv6 != "" {
		changes = append(changes,
			&route53.Change{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String(recordName),
					Type: aws.String("AAAA"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(devInfo.IPv6),
						},
					},
					TTL: aws.Int64(60),
				},
			},
		)
		log.Printf("Creating DDNS AAAA record %v with value %v", recordName, devInfo.IPv6)
	}

	params := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: changes,
			Comment: aws.String("Sample update."),
		},
		HostedZoneId: aws.String(*hostedZoneID),
	}
	_, err = r53.ChangeResourceRecordSets(params)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Record updated!")

}

func getDeviceInfo() deviceInfo {
	deviceName := os.Getenv("BALENA_DEVICE_NAME_AT_INIT")
	if deviceName == "" {
		deviceName, _ = os.Hostname()
	}
	if deviceName == "" {
		log.Fatal("Unable to determine device name")
	}
	deviceName = strings.ToLower(deviceName)
	log.Println("Detected device name", deviceName)
	log.Println("Looking for network interface on prefix match", *interfacePrefix)

	var interfaceIPv4 = ""
	var interfaceIPv6 = ""

	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if strings.HasPrefix(i.Name, *interfacePrefix) != true {
			continue
		}

		// found matching network interface
		log.Println("Using networking interface", i.Name)
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if interfaceIPv4 == "" {
				ipv4 := ip.To4()
				if ipv4 != nil {
					interfaceIPv4 = ipv4.String()
				}
			}
			if interfaceIPv6 == "" {
				ipv6 := ip.To16()
				if strings.Contains(ipv6.String(), ":") {
					interfaceIPv6 = ipv6.String()
				}
			}
		}
		break
	}
	log.Println("Using IPv4 address", interfaceIPv4)
	log.Println("Using IPv6 address", interfaceIPv6)

	return deviceInfo{name: deviceName, IPv4: interfaceIPv4, IPv6: interfaceIPv6}
}
