package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/robertn/dbx/internal/config"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoDriver struct {
	client *mongo.Client
}

func (d *mongoDriver) Connect(ctx context.Context, conn config.Connection) error {
	uri := conn.Host
	if !strings.HasPrefix(uri, "mongodb://") && !strings.HasPrefix(uri, "mongodb+srv://") {
		uri = "mongodb://"
		if conn.User != "" {
			uri += conn.User
			if conn.Password != "" {
				uri += ":" + conn.Password
			}
			uri += "@"
		}
		uri += conn.Host
		if conn.Port > 0 {
			uri += fmt.Sprintf(":%d", conn.Port)
		}
	}

	opts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(opts)
	if err != nil {
		return err
	}
	d.client = client
	return d.Ping(ctx)
}

func (d *mongoDriver) Close() error {
	if d.client != nil {
		return d.client.Disconnect(context.Background())
	}
	return nil
}

func (d *mongoDriver) Ping(ctx context.Context) error {
	var result bson.M
	return d.client.Database("admin").RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Decode(&result)
}

func (d *mongoDriver) Databases(ctx context.Context) ([]string, error) {
	dbs, err := d.client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	return dbs, nil
}

func (d *mongoDriver) Tables(ctx context.Context, database string) ([]string, error) {
	if database == "" {
		return nil, fmt.Errorf("no database selected")
	}
	colls, err := d.client.Database(database).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	return colls, nil
}

func (d *mongoDriver) Views(ctx context.Context, database string) ([]string, error) {
	return []string{}, nil // Views not separately handled for now
}

func (d *mongoDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	if database == "" {
		return nil, fmt.Errorf("no database selected")
	}
	coll := d.client.Database(database).Collection(table)
	var doc bson.M
	err := coll.FindOne(ctx, bson.D{}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return []ColumnInfo{{Name: "_id", DataType: "ObjectId"}}, nil
		}
		return nil, err
	}

	var cols []ColumnInfo
	for k, v := range doc {
		dataType := fmt.Sprintf("%T", v)
		cols = append(cols, ColumnInfo{Name: k, DataType: dataType})
	}
	// Sort to keep _id first, then alphabetical
	sort.Slice(cols, func(i, j int) bool {
		if cols[i].Name == "_id" {
			return true
		}
		if cols[j].Name == "_id" {
			return false
		}
		return cols[i].Name < cols[j].Name
	})

	return cols, nil
}

func (d *mongoDriver) AllTableColumns(ctx context.Context, database string) ([]TableColumn, error) {
	// A full scan is expensive. We just return nothing or a dummy for now.
	return []TableColumn{}, nil
}

func (d *mongoDriver) PrimaryKeyColumns(ctx context.Context, database, schema, table string) ([]string, error) {
	return []string{"_id"}, nil
}

func (d *mongoDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	var result bson.M
	err := d.client.Database(database).RunCommand(ctx, bson.D{{Key: "listIndexes", Value: table}}).Decode(&result)
	if err != nil {
		return "-- Collection: " + table, nil
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

func (d *mongoDriver) Exec(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	return d.Query(ctx, database, sqlStr)
}

func (d *mongoDriver) Query(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	if database == "" {
		return nil, fmt.Errorf("no database selected")
	}

	sqlTrimmed := strings.TrimSpace(sqlStr)
	if sqlTrimmed == "" {
		return nil, fmt.Errorf("empty query")
	}

	var cmd bson.D
	err := bson.UnmarshalExtJSON([]byte(sqlTrimmed), true, &cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON command: %v", err)
	}

	var result bson.M
	err = d.client.Database(database).RunCommand(ctx, cmd).Decode(&result)
	if err != nil {
		return nil, err
	}

	// Try to extract documents from cursor.firstBatch if present
	var docs []bson.M

	getMap := func(v interface{}) bson.M {
		if m, ok := v.(bson.M); ok {
			return m
		}
		if d, ok := v.(bson.D); ok {
			m := make(bson.M, len(d))
			for _, e := range d {
				m[e.Key] = e.Value
			}
			return m
		}
		return nil
	}

	getArray := func(v interface{}) []interface{} {
		if a, ok := v.(bson.A); ok {
			return a
		}
		if a, ok := v.([]interface{}); ok {
			return a
		}
		return nil
	}

	cursorMap := getMap(result["cursor"])
	if cursorMap != nil {
		if firstBatch := getArray(cursorMap["firstBatch"]); firstBatch != nil {
			for _, doc := range firstBatch {
				if m := getMap(doc); m != nil {
					docs = append(docs, m)
				}
			}
		}
	} else {
		// Just show the result object itself
		docs = append(docs, result)
	}

	if len(docs) == 0 {
		return &QueryResult{
			Columns: []string{"Result"},
			Rows:    [][]string{{"OK (0 documents returned)"}},
		}, nil
	}

	// Collect all distinct keys across all documents to form columns
	keySet := make(map[string]struct{})
	for _, doc := range docs {
		for k := range doc {
			keySet[k] = struct{}{}
		}
	}
	var columns []string
	hasID := false
	for k := range keySet {
		if k == "_id" {
			hasID = true
		} else {
			columns = append(columns, k)
		}
	}
	sort.Strings(columns)
	if hasID {
		columns = append([]string{"_id"}, columns...)
	}

	var rows [][]string
	for _, doc := range docs {
		var row []string
		for _, col := range columns {
			if val, ok := doc[col]; ok {
				if val == nil {
					row = append(row, "null")
					continue
				}
				switch v := val.(type) {
				case string:
					row = append(row, v)
				case bson.M, bson.A, map[string]interface{}, []interface{}:
					b, _ := json.Marshal(v)
					row = append(row, string(b))
				default:
					row = append(row, fmt.Sprintf("%v", v))
				}
			} else {
				row = append(row, "") // missing field
			}
		}
		rows = append(rows, row)
	}

	return &QueryResult{
		Columns: columns,
		Rows:    rows,
	}, nil
}
