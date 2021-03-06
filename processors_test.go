package main

import (
	"os"
	"os/user"
	"path"
	"testing"
)

const TEST_DOMAIN = "ohphiuhi.txt"
const TEST_ABUSE_EMAIL = "t@ohphiuhi.txt"

// Set config stuff here
func TestMain(m *testing.M) {
	loggedUser, err := user.Current()
	if err != nil {
		panic(err)
	}

	config.DatabasePath = "/tmp/dnsapi_test_database.sqlite"
	os.Remove(config.DatabasePath)

	config.PrimaryNameServer = "ns1.rosti.cz"
	config.NameServers = []string{
		"ns1.rosti.cz",
		"ns2.rosti.cz",
	}
	config.AbuseEmail = "cx@initd.cz"
	config.PrimaryNameServerIP = "1.2.3.4"
	config.SecondaryNameServerIPs = []string{"5.6.7.8"}
	config.SSHKey = path.Join(loggedUser.HomeDir, ".ssh/id_rsa")

	db := GetDatabaseConnection()
	defer db.Close()

	code := m.Run()

	os.Exit(code)
}

func TestNewZone(t *testing.T) {
	db := GetDatabaseConnection()

	zone, errs := NewZone("A-"+TEST_DOMAIN, []string{"test_tag_1", "test_tag_2"}, TEST_ABUSE_EMAIL)
	if len(errs) > 0 {
		t.Error(errs)
	}

	_, errs = UpdateZone(zone.ID, []string{"only_one_tag"}, "test@initd.cz")
	if len(errs) > 0 {
		t.Error(errs)
	}

	var updatedZone Zone
	err := db.Where("id = ?", zone.ID).Find(&updatedZone).Error
	if err != nil {
		t.Error(err)
	}

	record, errs := NewRecord(updatedZone.ID, "test", 3600, "A", 0, "1.2.3.4")
	if len(errs) > 0 {
		t.Error(errs)
	}

	record, errs = NewRecord(updatedZone.ID, "test2", 3600, "A", 0, "1.2.3.6")
	if len(errs) > 0 {
		t.Error(errs)
	}

	_, errs = UpdateRecord(record.ID, "test2", 600, 0, "1.2.3.5")
	if len(errs) > 0 {
		t.Error(errs)
	}

	// TODO: we need a mock server for this
	//err = Commit(updatedZone.ID)
	//if err != nil {
	//	t.Error(err)
	//}

	err = DeleteRecord(record.ID)
	if err != nil {
		t.Error(err)
	}
	var count = -1
	db.Model(&record).Where("id = ?", record.ID).Count(&count)
	if count != 0 {
		t.Error("Record still exist")
	}

	//err = DeleteZone(updatedZone.ID)
	//if err != nil {
	//	t.Error(err)
	//}
	//db.Where("id = ?", zone.ID).Find(&zone)
	//if zone.Delete != true {
	//	t.Error("Zone doesn't have delete flag")
	//}
}
