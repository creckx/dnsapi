package main

import (
	"bytes"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/errors"
)

// Record struct

type Record struct {
	ID        uint      `json:"id" gorm:"primary_key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ZoneId uint `json:"-" sql:"index"`

	Name  string `json:"name"`
	TTL   int    `json:"ttl"`
	Type  string `json:"type"` // A, AAAA, CNAME, TXT, SRV
	Prio  int    `json:"prio"`
	Value string `json:"value"`
}

// Validates the record
func (r *Record) Validate() error {
	// Test name
	matched, err := regexp.MatchString(`[a-z\.0-9@\-]{1,254}`, r.Value)
	if err != nil {
		panic(err)
	}
	if !matched {
		return errors.New(r.Type + " " + r.Name + ": name of the record is not in valid format")
	}

	// Test TTL
	if r.TTL < 60 || r.TTL > 2592000 {
		return errors.New(r.Type + " " + r.Name + ": TTL has to be number between 60 and 2592000")
	}

	// Test the rest
	if r.Type == "A" {
		parsed := net.ParseIP(r.Value)

		if parsed == nil || !strings.Contains(r.Value, ".") {
			return errors.New(r.Type + " " + r.Name + ": IP address of A record is not valid")
		}
	} else if r.Type == "AAAA" {
		parsed := net.ParseIP(r.Value)

		if parsed == nil || !strings.Contains(r.Value, ":") {
			return errors.New(r.Type + " " + r.Name + ": IP address of AAAA record is not valid")
		}
	} else if r.Type == "CNAME" {
		matched, err := regexp.MatchString(`[a-z\.0-9@\-]{1,254}`, r.Value)
		if err != nil {
			panic(err)
		}
		if !matched {
			return errors.New(r.Type + " " + r.Name + ": CNAME has not a valid value")
		}
	} else if r.Type == "TXT" {
		if strings.Contains(r.Value, "\"") || strings.Contains(r.Value, "'") || strings.Contains(r.Value, "`") {
			return errors.New(r.Type + " " + r.Name + ": characters \"' or ` are not allowed in TXT records")
		}
	} else if r.Type == "SRV" {
	} else if r.Type == "MX" {
		if r.Prio <= 0 && r.Prio <= 100 {
			return errors.New(r.Type + " " + r.Name + ": Prio has to be bigger than 0 and smaller than 100")
		}
		//TODO: Has to be domain and valid A/AAAA record (even in different location)
	} else {
		return errors.New("Unknown record type")
	}

	return nil
}

// Render renders one record
func (r *Record) Render() string {
	var value = r.Value

	// In case of TXT, we have to split large records into lines
	if r.Type == "TXT" {
		var part = 254
		var length = len(r.Value)
		var last = length % part
		var parts []string

		for current := 0; current < length; current += part {
			if current+part > length {
				parts = append(parts, r.Value[current:current+last])
			} else {
				parts = append(parts, r.Value[current:current+part])
			}
		}

		value = "(\"" + strings.Join(parts, "\"\n        \"") + "\")"
	}

	// If the record is MX, add prio
	if r.Type == "MX" {
		return r.Name + "    " +
			strconv.Itoa(r.TTL) + "s    " +
			r.Type + "  " +
			strconv.Itoa(r.Prio) + "    " +
			value
	} else {
		return r.Name + "    " +
			strconv.Itoa(r.TTL) + "s    " +
			r.Type + "      " +
			value
	}
}

// Zone struct

type Zone struct {
	ID        uint      `json:"id" gorm:"primary_key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Delete    bool      `json:"delete" gorm:"DEFAULT:0"`

	Domain     string   `json:"domain" sql:"index"`
	Serial     string   `json:"serial"`
	Records    []Record `json:"records" gorm:"foreignkey:ZoneID"`
	Tags       string   `json:"tags"` // Tags separated by comma
	AbuseEmail string   `json:"abuse_email"`
}

func (z *Zone) SetNewSerial() {
	today := time.Now().UTC().Format("20060102")

	if z.Serial == "" || len(z.Serial) != 10 {
		z.Serial = today + "01"
	} else {
		if today == z.Serial[0:8] {
			number, err := strconv.Atoi(z.Serial[8:10])
			if err != nil {
				panic(err)
			}
			z.Serial = today + fmt.Sprintf("%02d", number+1)
		} else {
			z.Serial = today + "01"
		}
	}
}

func (z *Zone) RenderAbuseEmail() string {
	if z.AbuseEmail == "" {
		return config.RenderEmail()
	} else {
		return strings.Replace(z.AbuseEmail, "@", ".", -1)
	}
}

func (z *Zone) AddRecord(name string, ttl int, recordType string, prio int, value string) (*Record, []error) {
	if z.ID == 0 {
		return nil, []error{errors.New("zone is not saved")}
	}

	var record = Record{
		ZoneId: z.ID,
		Name:   name,
		TTL:    ttl,
		Type:   recordType,
		Prio:   prio,
		Value:  value,
	}

	z.Records = append(z.Records, record)

	return &record, z.Validate()
}

// Validates records in the zone
func (z *Zone) Validate() []error {
	var errorsMsgs []error
	var usedNames []string

	var numberOfExistingDomains int
	db := GetDatabaseConnection()
	err := db.Model(z).Where("domain = ?", z.Domain).Count(&numberOfExistingDomains).Error
	if err != nil {
		panic(err)
	}

	if z.ID == 0 && numberOfExistingDomains > 0 {
		errorsMsgs = append(errorsMsgs, errors.New("domain already exists"))
	}

	for _, record := range z.Records {
		err := record.Validate()
		if err != nil {
			errorsMsgs = append(errorsMsgs, err)
		}

		if record.Type == "A" || record.Type == "AAAA" || record.Type == "CNAME" {
			usedNames = append(usedNames, record.Name)
		}
	}

	// Additional checks

	// Domain name has to exist
	// TODO: make this better
	if len(z.Domain) < 3 {
		errorsMsgs = append(errorsMsgs, errors.New("domain name has to be longer than three characters"))
	}

	if !strings.Contains(z.Domain, ".") {
		errorsMsgs = append(errorsMsgs, errors.New("domain name has to contain at least one dot"))
	}

	// CNAME record can't have same name as another AAAA record, A record or CNAME record
	for _, record := range z.Records {
		if record.Type == "CNAME" {
			count := 0
			for _, usedName := range usedNames {
				if usedName == record.Name {
					count += 1
				}
			}
			if count > 1 {
				errorsMsgs = append(errorsMsgs, errors.New(record.Type+" "+record.Name+" is already used in another A/AAAA/CNAME record"))
			}
		}
	}

	return errorsMsgs
}

// Renders whole zone
func (z *Zone) Render() string {
	var zone string

	/*
		@     IN     SOA    <primary-name-server>	<hostmaster-email> (
		<serial-number>
		<time-to-refresh>
		<time-to-retry>
		<time-to-expire>
		<minimum-TTL> )
	*/

	zone = `$TTL ` + strconv.Itoa(config.TTL) + `s
@       IN      SOA     ` + config.PrimaryNameServer + `. ` + z.RenderAbuseEmail() + `.  (
		` + z.Serial + `
		` + strconv.Itoa(config.TimeToRefresh) + `
		` + strconv.Itoa(config.TimeToRetry) + `
		` + strconv.Itoa(config.TimeToExpire) + `
		` + strconv.Itoa(config.MinimalTTL) + `
)
`
	for _, nameserver := range config.NameServers {
		zone += "@    IN    NS    " + nameserver + ".\n"
	}
	//zone += "\n"

	for _, record := range z.Records {
		zone += record.Render()
		zone += "\n"
	}

	return zone
}

func (z *Zone) RenderPrimary() string {
	primaryTemplate := `zone "{{ .Domain }}" IN {
        type master;
        masterfile-format text;
        file "{{ .Domain }}.zone";
        allow-query { any; };
        allow-transfer { {{ .AllowTransfer}}; };
        notify yes;
};
`

	tmpl, err := template.New("").Parse(primaryTemplate)
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct {
		Domain        string
		AllowTransfer string
	}{
		Domain:        z.Domain,
		AllowTransfer: strings.Join(config.SecondaryNameServerIPs, "; "),
	})

	if err != nil {
		panic(err)
	}

	return string(buf.Bytes())
}

func (z *Zone) RenderSecondary() string {
	secondaryTemplate := `zone "{{ .Domain }}" IN {
    type slave;
    masterfile-format text;
    file "{{ .Domain }}.zone";
    allow-query { any; };
    masters { {{ .Masters }}; };
};`
	tmpl, err := template.New("").Parse(secondaryTemplate)
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct {
		Domain  string
		Masters string
	}{
		Domain:  z.Domain,
		Masters: config.PrimaryNameServerIP,
	})

	if err != nil {
		panic(err)
	}

	return string(buf.Bytes())
}
