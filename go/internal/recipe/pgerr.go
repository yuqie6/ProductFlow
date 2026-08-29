package recipe

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func applicationKeyConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uq_workflow_recipe_applications_product_key"
}
