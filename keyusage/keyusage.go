package keyusage

import (
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/IPA-CyberLab/kmgm/pb"
)

type KeyUsage struct {
	KeyUsage     x509.KeyUsage
	ExtKeyUsages []x509.ExtKeyUsage
}

var KeyUsageCA = KeyUsage{
	KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	// https://tools.ietf.org/html/rfc5280#section-4.2.1.12
	// "In general, this extension will appear only in end entity certificates."
	ExtKeyUsages: nil,
}

var KeyUsageTLSServer = KeyUsage{
	KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	ExtKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
}

var KeyUsageTLSClient = KeyUsage{
	KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	ExtKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
}

var KeyUsageTLSClientServer = KeyUsage{
	KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	ExtKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
}

func (u KeyUsage) Clone() KeyUsage {
	return KeyUsage{
		KeyUsage:     u.KeyUsage,
		ExtKeyUsages: append([]x509.ExtKeyUsage{}, u.ExtKeyUsages...),
	}
}

type yamlKeyUsage struct {
	KeyUsage    []string `yaml:"keyUsage"`
	ExtKeyUsage []string `yaml:"extKeyUsage"`
	Preset      string   `yaml:"preset"`
}

func PresetFromString(s string) (KeyUsage, error) {
	if s == "tlsServer" {
		return KeyUsageTLSServer.Clone(), nil
	} else if s == "tlsClient" {
		return KeyUsageTLSClient.Clone(), nil
	} else if s == "tlsClientServer" {
		return KeyUsageTLSClientServer.Clone(), nil
	} else {
		return KeyUsage{}, fmt.Errorf("Unknown preset %q specified", s)
	}
}

func (u *KeyUsage) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var yku yamlKeyUsage
	if err := unmarshal(&yku); err != nil {
		return err
	}

	if yku.Preset != "" {
		if len(yku.KeyUsage) != 0 {
			return errors.New("preset and keyUsage is not allowed to be specified at once.")
		}
		if len(yku.ExtKeyUsage) != 0 {
			return errors.New("preset and extKeyUsage is not allowed to be specified at once.")
		}

		var err error
		*u, err = PresetFromString(yku.Preset)
		if err != nil {
			return err
		}
		return nil
	}

	u.KeyUsage = x509.KeyUsage(0)
	for _, ku := range yku.KeyUsage {
		// FIXME[P2]: Support more

		if ku == "keyEncipherment" {
			u.KeyUsage |= x509.KeyUsageKeyEncipherment
		} else if ku == "digitalSignature" {
			u.KeyUsage |= x509.KeyUsageDigitalSignature
		} else {
			return fmt.Errorf("Unknown keyUsage %q", ku)
		}
	}

	foundAny := false
	u.ExtKeyUsages = []x509.ExtKeyUsage{}
	for _, eku := range yku.ExtKeyUsage {
		// FIXME[P2]: Support more

		if eku == "any" {
			foundAny = true
			u.ExtKeyUsages = append(u.ExtKeyUsages, x509.ExtKeyUsageAny)
		} else if eku == "clientAuth" {
			u.ExtKeyUsages = append(u.ExtKeyUsages, x509.ExtKeyUsageClientAuth)
		} else if eku == "serverAuth" {
			u.ExtKeyUsages = append(u.ExtKeyUsages, x509.ExtKeyUsageServerAuth)
		}
	}
	if foundAny && len(u.ExtKeyUsages) > 1 {
		return fmt.Errorf("extKeyUsage \"any\" and other extKeyUsages cannot be specified at once.")
	}

	return nil
}

func FromProtoStruct(s *pb.KeyUsage) KeyUsage {
	if s == nil {
		return KeyUsage{}
	}

	ekus := make([]x509.ExtKeyUsage, 0, len(s.ExtKeyUsages))
	for _, ekuint := range s.ExtKeyUsages {
		ekus = append(ekus, x509.ExtKeyUsage(ekuint))
	}

	return KeyUsage{
		KeyUsage:     x509.KeyUsage(s.KeyUsage),
		ExtKeyUsages: ekus,
	}
}

func (u KeyUsage) ToProtoStruct() *pb.KeyUsage {
	ekuints := make([]uint32, 0, len(u.ExtKeyUsages))
	for _, eku := range u.ExtKeyUsages {
		ekuints = append(ekuints, uint32(eku))
	}

	return &pb.KeyUsage{
		KeyUsage:     uint32(u.KeyUsage),
		ExtKeyUsages: ekuints,
	}
}

func FromCertificate(cert *x509.Certificate) KeyUsage {
	return KeyUsage{
		KeyUsage:     cert.KeyUsage,
		ExtKeyUsages: cert.ExtKeyUsage,
	}
}

func (p *KeyUsage) UnmarshalFlag(s string) error {
	ku, err := PresetFromString(s)
	if err != nil {
		return err
	}

	*p = ku
	return nil
}