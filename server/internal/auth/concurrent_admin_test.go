package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresConcurrentAdminLoss serializes both admin-loss transactions
// after their count checks on the current implementation. The trigger only
// sleeps for these two disposable fixture users, giving the second
// transaction time to observe the still-uncommitted first role deletion.
// A correct transaction-wide lock makes one request wait before counting,
// so exactly one demotion succeeds and one returns ErrLastAdmin.
func TestPostgresConcurrentAdminLoss(t *testing.T) {
	url := os.Getenv("KURA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KURA_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewService(NewPostgresStore(pool))
	actor := Actor{ID: SystemActorID, System: true}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	users := make([]User, 0, 2)
	for _, handle := range []string{"racea" + suffix, "raceb" + suffix} {
		user, createErr := svc.CreateUser(ctx, actor, handle, testPassword, "admin")
		if createErr != nil {
			t.Fatal(createErr)
		}
		users = append(users, user)
	}
	function := "kura_test_admin_pause_" + suffix
	trigger := "kura_test_admin_pause_trigger_" + suffix
	defer func() {
		_, _ = pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+trigger+" ON user_roles")
		_, _ = pool.Exec(context.Background(), "DROP FUNCTION IF EXISTS public."+function+"()")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []int64{users[0].ID, users[1].ID})
	}()
	ddl := fmt.Sprintf(`CREATE FUNCTION public.%s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.user_id IN (%d, %d) THEN PERFORM pg_sleep(0.4); END IF;
  RETURN OLD;
END
$$`, function, users[0].ID, users[1].ID)
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE TRIGGER %s BEFORE DELETE ON user_roles FOR EACH ROW EXECUTE FUNCTION public.%s()", trigger, function)); err != nil {
		t.Fatal(err)
	}

	viewer := "viewer"
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range users {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.UpdateUser(ctx, actor, users[i].ID, UpdateUserPatch{Role: &viewer})
		}(i)
	}
	close(start)
	wg.Wait()

	successes, lastAdminErrors := 0, 0
	for _, updateErr := range errs {
		if updateErr == nil {
			successes++
		} else if errors.Is(updateErr, ErrLastAdmin) {
			lastAdminErrors++
		} else {
			t.Fatalf("unexpected concurrent update error: %v", updateErr)
		}
	}
	if successes != 1 || lastAdminErrors != 1 {
		t.Fatalf("concurrent admin demotions: successes=%d last-admin rejections=%d; want 1 each (errors: %v)", successes, lastAdminErrors, errs)
	}
}
