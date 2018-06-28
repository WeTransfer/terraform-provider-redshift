package redshift

import (
	"github.com/hashicorp/terraform/helper/schema"
	"database/sql"
	"log"
	"time"
)
/*
TODO
Add database property. This will require a new connection since you can't have databse agnostic connections in redshift/postgres
 */

func redshiftSchema() *schema.Resource {
	return &schema.Resource{
		Create: resourceRedshiftSchemaCreate,
		Read:   resourceRedshiftSchemaRead,
		Update: resourceRedshiftSchemaUpdate,
		Delete: resourceRedshiftSchemaDelete,
		Exists: resourceRedshiftSchemaExists,
		Importer: &schema.ResourceImporter{
			State: resourceRedshiftSchemaImport,
		},

		Schema: map[string]*schema.Schema{
			"schema_name": { //This isn't immutable. The usesysid returned should be used as the id
				Type:     schema.TypeString,
				Required: true,
			},
			"owner": {
				Type:     schema.TypeInt,
				Optional: true,
			},
			"cascade_on_delete": {
				Type:     schema.TypeBool,
				Optional: true,
				Description: "Keyword that indicates to automatically drop all objects in the schema, such as tables and functions. By default it doesn't for your safety",
				Default:  false,
			},
		},
	}
}

func resourceRedshiftSchemaExists(d *schema.ResourceData, meta interface{}) (b bool, e error) {
	// Exists - This is called to verify a resource still exists. It is called prior to Read,
	// and lowers the burden of Read to be able to assume the resource exists.
	client := meta.(*Client).db

	var name string

	err := client.QueryRow("SELECT nspname FROM pg_namespace WHERE oid = $1", d.Id()).Scan(&name)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

func resourceRedshiftSchemaCreate(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*Client).db

	var createStatement string = "CREATE SCHEMA " + d.Get("schema_name").(string)

	//If no owner is specified it defaults to client user
	if v, ok := d.GetOk("owner"); ok {
		var usernames = GetUsersnamesForUsesysid(redshiftClient, []interface{}{v.(int)})
		createStatement += " AUTHORIZATION " + usernames[0]
	}

	log.Print("Create Schema statement: " + createStatement)

	if _, err := redshiftClient.Exec(createStatement); err != nil {
		log.Fatal(err)
		return err
	}

	//The changes do not propagate instantly
	time.Sleep(5 * time.Second)

	var oid string

	err := redshiftClient.QueryRow("SELECT oid FROM pg_namespace WHERE nspname = $1", d.Get("shema_name").(string)).Scan(&oid)

	if err != nil {
		log.Fatal(err)
		return err
	}

	d.SetId(oid)

	readErr := readRedshiftSchema(d, redshiftClient)

	return readErr
}

func resourceRedshiftSchemaRead(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*Client).db

	err := readRedshiftSchema(d, redshiftClient)

	return err
}

func readRedshiftSchema(d *schema.ResourceData, db *sql.DB) error {
	var (
		schema_name  string
		owner        int
	)

	err := db.QueryRow("select nspname, nspowner from pg_namespace where oid = $1", d.Id()).Scan(&schema_name, &owner)

	if err != nil {
		log.Fatal(err)
		return err
	}

	d.Set("schema_name", schema_name)
	d.Set("owner", owner)

	return nil
}

func resourceRedshiftSchemaUpdate(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*Client).db
	tx, txErr := redshiftClient.Begin()
	if txErr != nil {
		panic(txErr)
	}

	if d.HasChange("schema_name") {

		oldName, newName := d.GetChange("schema_name")
		alterSchemaNameQuery := "ALTER SCHEMA " + oldName.(string) + " RENAME TO " + newName.(string)

		if _, err := tx.Exec(alterSchemaNameQuery); err != nil {
			return err
		}
	}

	if d.HasChange("owner") {

		var username = GetUsersnamesForUsesysid(redshiftClient, []interface{}{d.Get("owner").(int)})

		if _, err := tx.Exec("ALTER SCHEMA " + d.Get("schema_name").(string) + " OWNER TO " + username[0]); err != nil {
			return err
		}
	}

	err := readRedshiftSchema(d, redshiftClient)

	if err == nil {
		tx.Commit()
		return nil
	} else {
		tx.Rollback()
		return err
	}
}

func resourceRedshiftSchemaDelete(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*Client).db

	dropSchemaQuery := "DROP SCHEMA " + d.Get("schema_name").(string)

	if v, ok := d.GetOk("cascade_on_delete"); ok && v.(bool) {
		dropSchemaQuery += " CASCADE "
	}

	_, err := client.Exec(dropSchemaQuery)

	if err != nil {
		log.Fatal(err)
		return err
	}

	return nil
}

func resourceRedshiftSchemaImport(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	if err := resourceRedshiftSchemaRead(d, meta); err != nil {
		return nil, err
	}
	return []*schema.ResourceData{d}, nil
}

func GetSchemanNameForSchemaId(q Queryer, schemaId int) (string, error) {

	var name string

	err := q.QueryRow("SELECT nspname FROM pg_namespace WHERE oid = $1", schemaId).Scan(&name)
	switch {
	case err == sql.ErrNoRows:
		//Is this a good idea?
		return "", err
	case err != nil:
		return "", err
	}
	return name, nil
}