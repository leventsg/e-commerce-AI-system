// Package testutil contains checkout test fixtures, never runtime dependencies.
package testutil

import (
	"database/sql/driver"
	"github.com/DATA-DOG/go-sqlmock"
	model "github.com/leventsg/e-commerce-AI-system/dal/model/checkout"
	"reflect"
)

func Rows(row *model.Checkouts) *sqlmock.Rows {
	value := reflect.ValueOf(*row)
	columns := make([]string, value.NumField())
	values := make([]driver.Value, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		columns[i] = value.Type().Field(i).Tag.Get("db")
		converted, err := driver.DefaultParameterConverter.ConvertValue(value.Field(i).Interface())
		if err != nil {
			panic(err)
		}
		values[i] = converted
	}
	return sqlmock.NewRows(columns).AddRow(values...)
}
