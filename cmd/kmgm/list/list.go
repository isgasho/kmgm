package list

import (
	"crypto/x509"
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/IPA-CyberLab/kmgm/action"
	"github.com/IPA-CyberLab/kmgm/storage"
	"github.com/IPA-CyberLab/kmgm/storage/issuedb"
)

// FIXME[PX]: [DB op] list all issued certs
// FIXME[PX]: [DB op] check that issued certs expires > threshold

const dateFormat = "06/01/02"

func CertInfo(c *x509.Certificate) string {
	return fmt.Sprintf("%s %s %v",
		c.NotBefore.Format(dateFormat),
		c.NotAfter.Format(dateFormat),
		c.Subject)
}

func LsProfile(env *action.Environment) error {
	now := env.NowImpl()

	ps, err := env.Storage.Profiles()
	if err != nil {
		return fmt.Errorf("Failed to list profiles: %w", err)
	}

	for _, p := range ps {
		fmt.Printf("%s %s\n", p.Name(), p.Status(now))
	}

	return nil
}

var Command = &cli.Command{
	Name:    "list",
	Usage:   "List certificates issued",
	Aliases: []string{"ls"},
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "profile",
			Aliases: []string{"p", "profiles"},
			Usage:   "Print list of profiles",
		},
	},
	Action: func(c *cli.Context) error {
		env := action.GlobalEnvironment
		slog := env.Logger.Sugar()

		// FIXME[P3]: Unless verbose, omit time/level logging as well

		if c.Bool("profile") {
			if err := LsProfile(env); err != nil {
				return err
			}
			return nil
		}

		profile, err := env.Profile()
		if err != nil {
			return err
		}

		now := env.NowImpl()
		st := profile.Status(now)
		if st.Code != storage.ValidCA {
			if st.Code == storage.Expired {
				slog.Warnf("Expired %s")
			} else {
				slog.Infof("Could not find a valid CA profile %q: %v", env.ProfileName, st)
				return nil
			}
		}

		db, err := issuedb.New(profile.IssueDBPath())
		if err != nil {
			return err
		}

		es, err := db.Entries()
		if err != nil {
			return err
		}

		//                    1         2         3         4         5         6         7
		//          01234567890123456789012345678901234567890123456789012345678901234567890123456789
		//          01234567 0123456789012345678
		fmt.Printf("                             YY/MM/DD YY/MM/DD\n")
		fmt.Printf("Status   SerialNumber        NotBefor NotAfter Subject\n")
		for _, e := range es {
			cert, err := e.ParseCertificate()
			var infotxt string
			if err != nil {
				infotxt = fmt.Sprintf("error: Failed to parse PEM: %v", err)
			} else {
				infotxt = CertInfo(cert)
			}

			switch e.State {
			case issuedb.IssueInProgress:
				fmt.Printf("issueing %19d\n", e.SerialNumber)

			case issuedb.ActiveCertificate:
				fmt.Printf("active   %19d %s\n", e.SerialNumber, infotxt)
			}
		}

		return nil
	},
}
