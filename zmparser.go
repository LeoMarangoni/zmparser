package main

import "fmt"
import "log"
import "os"
import "flag"
import "strings"
import "io/ioutil"
import "encoding/xml"
import "crypto/tls"
import "gopkg.in/ldap.v2"

//Parametrization
type attrArray []string

func (i *attrArray) String() string {
	return "my string representation"
}

func (i *attrArray) Set(value string) error {
	*i = append(*i, value)
	return nil
}

var attrs attrArray

var showDomain = flag.Bool("domain", false, "Show Domain in first Column")
var useCOSId = flag.Bool("nocosname", false, "Use COS Id istead Name")


//Zimbra Localconfig structs
type Localconfig struct {
    XMLName xml.Name `xml:"localconfig"`
    Localconfig   []Key   `xml:"key"`
}

type Key struct {
	Name string `xml:"name,attr"`
	Value string `xml:"value"`
}


func main() {
    //Init parser
    flag.Var(&attrs, "add", "Add custom attributes ")
    flag.Parse()

	//Open zimbra localconfig
	xmlFile, err := os.Open("/opt/zimbra/conf/localconfig.xml")

	if err != nil {
        log.Fatal(err)
    }
    
	log.Println("Successfully Opened Zimbra Localconfig")
    log.Println("Getting keys for ldapsearch")
	// defer the closing of our xmlFile so that we can parse it later on
	defer xmlFile.Close()
	
	byteValue, _ := ioutil.ReadAll(xmlFile)

	var localconfig Localconfig

	xml.Unmarshal(byteValue, &localconfig)

	//Get the keys we need for ldapsearch
	var ldap_host, ldap_user, ldap_password, ldap_port string
	for _, s := range localconfig.Localconfig {
	
		if s.Name == "ldap_host" {ldap_host = s.Value}
		if s.Name == "ldap_port" {ldap_port = s.Value}
		if s.Name == "zimbra_ldap_userdn" {ldap_user = s.Value}
		if s.Name == "zimbra_ldap_password" {ldap_password = s.Value}
	}

    //Start LDAP communication
    l, err := ldap.Dial("tcp", fmt.Sprintf("%s:%s", ldap_host, ldap_port))
    if err != nil {
        log.Fatal(err)
    }
    defer l.Close()

    // Reconnect with TLS
    err = l.StartTLS(&tls.Config{InsecureSkipVerify: true})
    if err != nil {
        log.Fatal(err)
    }

    // First bind with a read only user
    err = l.Bind(ldap_user, ldap_password)
    if err != nil {
        log.Fatal(err)
    }

    // Search COS
    searchRequest := ldap.NewSearchRequest(
        "",
        ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
        "(&(objectClass=zimbraCOS))",
        []string{"zimbraId", "cn"},
        nil,
    )

    cosResult, err := l.Search(searchRequest)
    if err != nil {
        log.Fatal(err)
    }

	cos_by_id := make(map[string]string)
	cos_by_name := make(map[string]string)


	for _, cos := range cosResult.Entries {
		cos_id := cos.GetAttributeValue("zimbraId")
		cos_name := cos.GetAttributeValue("cn")
		cos_by_id[cos_id] = cos_name
		cos_by_name[cos_name] = cos_id
	}


	//search Domains with zimbraDomainDefaultCOSId
    searchRequest = ldap.NewSearchRequest(
        "",
        ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
        "(&(objectClass=zimbraDomain)(zimbraDomainDefaultCOSId=*))",
        []string{"zimbraDomainName", "zimbraDomainDefaultCOSId"},
        nil,
    )

    domResult, err := l.Search(searchRequest)
    if err != nil {
        log.Fatal(err)
    }

	domain_default_cos := make(map[string]string)

	for _, dom := range domResult.Entries {
		dom_name := dom.GetAttributeValue("zimbraDomainName")
		domain_default_cos[dom_name] = dom.GetAttributeValue("zimbraDomainDefaultCOSId")
	}

    //search for accounts
    zimbraAttrs := []string{"mail", "zimbraCOSId"}
    for _, item := range attrs {
        zimbraAttrs = append(zimbraAttrs, item)
    }
	searchRequest = ldap.NewSearchRequest(
        "",
        ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
        "(&(objectClass=zimbraAccount)(mail=*)(!(|(zimbraIsSystemAccount=TRUE)(objectClass=zimbraCalendarResource))))",
        zimbraAttrs,
        nil,
    )

    accResult, err := l.Search(searchRequest)
    if err != nil {
        log.Fatal(err)
	}
    if *showDomain {
        fmt.Printf("domain,")
    }    
    fmt.Printf("email,")
    fmt.Printf("COS,")
    for _, item := range attrs {
        fmt.Printf("%s,", item)
    }
    fmt.Printf("\n")
	for _, acc := range accResult.Entries {
		mail := acc.GetAttributeValue("mail")
		domain := strings.Split(mail, "@")[1]
		cos_id := acc.GetAttributeValue("zimbraCOSId")
		//Map COS id for Names
		if cos_id == "" {
			domain_cos := domain_default_cos[domain]
			if domain_cos != "" {
				cos_id = domain_cos
			} else {
				cos_id = cos_by_name["default"]
			}
		}
        cos_name := cos_by_id[cos_id]
        
        //fmt.Printf("%s,%s,%s\n", domain, mail, cos_name)
        if *showDomain {
            fmt.Printf("%s,", domain)
        }
        fmt.Printf("%s,", mail)
        if *useCOSId {
            fmt.Printf("%s,", cos_id)
        } else {
            fmt.Printf("%s,", cos_name)
        }
        for _, item := range attrs {
            fmt.Printf("%s,", acc.GetAttributeValue(item))
        }
        fmt.Printf("\n")
	}
}
