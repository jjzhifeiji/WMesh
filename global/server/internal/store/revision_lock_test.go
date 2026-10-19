// 修订号并发：改厂状态、绑定与改绑在行锁下排队，修订不重号，注销不被改回。
package store_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

// 每个用例跑的轮数，首轮顺带拨好连接，后几轮真撞在一起。
const revisionRounds = 12

// 验同一厂并发注销与停用或启用，最终仍是注销，修订连续不重号，交回的原状态接得上。
func TestSetFactoryStatusConcurrentRetire(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 自己拿库连接，后面要调空闲连接数。
	db := testpg.Fresh(t)
	// 用刚才那份库，名录都写进这里。
	s := store.Open(db)
	// 取出底层连接池，才能调空闲连接数。
	sqlDB, err := db.DB()
	// 拿不到连接池就停，没法调空闲数。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 空闲连接留够，两路不用现拨号，写入才撞得上。
	sqlDB.SetMaxIdleConns(4)
	// 每轮换一家新厂，偶数轮从有效起，奇数轮从停用起。
	for round := range revisionRounds {
		// 每轮登记一家新厂，登录名和建厂码都带轮号免撞。
		fac, err := s.RegisterFactory(ctx, id.New(), fmt.Sprintf("厂R%d", round), id.New(),
			fmt.Sprintf("sa-r%d", round), "超管", fmt.Sprintf("enroll-r%d", round))
		// 厂没登记上就停，这一轮没有对象。
		if err != nil {
			// 带上轮号停测。
			t.Fatalf("round %d register: %v", round, err)
		}
		// 起点修订，偶数轮是零，奇数轮先停用一次成一。
		base := int64(0)
		// 和注销对撞的那一路，偶数轮停用、奇数轮启用。
		other := store.FactoryDisabled
		// 起点状态，偶数轮有效，奇数轮先停用成停用。
		start := store.FactoryActive
		// 奇数轮先停用，再拿启用去和注销对撞。
		if round%2 == 1 {
			// 先停用一次，起点修订升到一。
			pre, _, err := s.SetFactoryStatus(ctx, fac.ID, store.FactoryDisabled)
			// 停用没写成或修订不对就停，起点不成立。
			if err != nil || pre.LifecycleRevision != 1 {
				// 带上轮号和结果停测。
				t.Fatalf("round %d pre-disable: %+v %v", round, pre, err)
			}
			// 起点修订记成一。
			base = 1
			// 这一轮拿启用去和注销对撞。
			other = store.FactoryActive
			// 起点状态记成停用。
			start = store.FactoryDisabled
		}
		// 两路目标：一路注销，一路停用或启用。
		targets := []string{store.FactoryRetired, other}
		// 每路结果各占一格，不用加锁。
		outs := make([]store.Factory, len(targets))
		// 每路锁内读到的原状态各占一格，不用加锁。
		prevs := make([]string, len(targets))
		// 每路错误各占一格，不用加锁。
		errs := make([]error, len(targets))
		// 两路同时改同一厂的状态。
		runRevisionRace(len(targets), func(i int) {
			// 这一路改到自己的目标状态，结果、原状态和错误各记一格。
			outs[i], prevs[i], errs[i] = s.SetFactoryStatus(context.Background(), fac.ID, targets[i])
		})
		// 注销那一路必须成功，从有效或停用都能注销。
		if errs[0] != nil || outs[0].Status != store.FactoryRetired {
			// 带上轮号和结果停测。
			t.Fatalf("round %d retire: %+v %v", round, outs[0], errs[0])
		}
		// 收下成功那几路的修订，后面核对连续不重号。
		revs := []int64{outs[0].LifecycleRevision}
		// 成功的那几路，后面按修订排好核对原状态接得上。
		won := []int{0}
		// 另一路要么改成，要么因已注销被拒，别的错都不对。
		switch {
		// 另一路改成了，状态必须是它的目标。
		case errs[1] == nil:
			// 交回的状态不是目标就停。
			if outs[1].Status != other {
				// 带上轮号和结果停测。
				t.Fatalf("round %d %s: %+v", round, other, outs[1])
			}
			// 这一路的修订也收下。
			revs = append(revs, outs[1].LifecycleRevision)
			// 这一路也成了，参与原状态核对。
			won = append(won, 1)
		// 注销在前，另一路被拒是对的。
		case errors.Is(errs[1], domain.ErrFactoryRetired):
		// 其余错误都不该出现。
		default:
			// 带上轮号和错误停测。
			t.Fatalf("round %d %s: %v", round, other, errs[1])
		}
		// 修订从小到大排好，再逐个比对。
		slices.Sort(revs)
		// 成功的修订必须从起点往上连续加一，不能重号。
		for i, rev := range revs {
			// 不是起点加序号就说明重号或跳号。
			if rev != base+int64(i)+1 {
				// 带上轮号和全部修订停测。
				t.Fatalf("round %d revisions %v, base %d", round, revs, base)
			}
		}
		// 成功的几路按修订从小到大排，就是真正提交的先后。
		slices.SortFunc(won, func(a, b int) int {
			// 修订小的先提交，排在前面。
			return cmp.Compare(outs[a].LifecycleRevision, outs[b].LifecycleRevision)
		})
		// 首个成功那路读到的原状态应是起点。
		want := start
		// 逐路核对锁内原状态就是前一笔提交后的状态。
		for _, i := range won {
			// 对不上说明交回的不是锁内读到的值。
			if prevs[i] != want {
				// 带上轮号、这一路和应有的原状态停测。
				t.Fatalf("round %d way %d prev %q, want %q", round, i, prevs[i], want)
			}
			// 下一路的原状态应是这一路改成的状态。
			want = outs[i].Status
		}
		// 读回库里的当前行，核对最终状态和修订。
		cur, err := s.FactoryByID(ctx, fac.ID)
		// 读失败、没停在注销、或修订不是最后一次都不对。
		if err != nil || cur.Status != store.FactoryRetired || cur.LifecycleRevision != revs[len(revs)-1] || cur.LifecycleRevision != outs[0].LifecycleRevision {
			// 带上轮号、当前行和全部修订停测。
			t.Fatalf("round %d final %+v revisions %v err %v", round, cur, revs, err)
		}
	}
}

// 验同一 Client 并发绑两家只成一家，并发改两家修订不重号、原厂接得上，最新的就是库里那家。
func TestRebindClientConcurrentRevision(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 自己拿库连接，后面要调空闲连接数。
	db := testpg.Fresh(t)
	// 用刚才那份库，设备和名录都写进这里。
	s := store.Open(db)
	// 取出底层连接池，才能调空闲连接数。
	sqlDB, err := db.DB()
	// 拿不到连接池就停，没法调空闲数。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 空闲连接留够，两路不用现拨号，写入才撞得上。
	sqlDB.SetMaxIdleConns(4)
	// 准备三家厂，绑定和改绑都在这三家里挑。
	facs := make([]uuid.UUID, 3)
	// 逐家登记，登录名和建厂码都带序号免撞。
	for i := range facs {
		// 登记一家厂，作为绑定或改绑的目标。
		fac, err := s.RegisterFactory(ctx, id.New(), fmt.Sprintf("厂%d", i), id.New(),
			fmt.Sprintf("sa-%d", i), "超管", fmt.Sprintf("enroll-%d", i))
		// 厂没登记上就停，后面没有目标厂。
		if err != nil {
			// 带上序号停测。
			t.Fatalf("register %d: %v", i, err)
		}
		// 记下这家厂的身份。
		facs[i] = fac.ID
	}
	// 每轮换一台新设备，先并发绑定，再并发改绑。
	for round := range revisionRounds {
		// 登记一台未分配设备，名字带轮号。
		c, err := s.CreateClient(ctx, id.New(), fmt.Sprintf("焊机-%d", round), nil, "")
		// 设备没登记上就停，这一轮没有对象。
		if err != nil {
			// 带上轮号停测。
			t.Fatalf("round %d create client: %v", round, err)
		}
		// 并发绑定的两家：前两家厂。
		bindTo := facs[:2]
		// 每路绑定结果各占一格，不用加锁。
		bound := make([]store.Client, len(bindTo))
		// 每路绑定错误各占一格，不用加锁。
		bindErrs := make([]error, len(bindTo))
		// 两路同时把这台设备绑到不同的厂。
		runRevisionRace(len(bindTo), func(i int) {
			// 这一路绑到自己的厂，结果和错误各记一格。
			bound[i], bindErrs[i] = s.BindClient(context.Background(), c.ID, bindTo[i])
		})
		// 记下绑成的那一路，必须恰好一路。
		winner := -1
		// 逐路看结果，成的记下，没成的必须是已绑定。
		for i := range bindTo {
			// 这一路没成，只能因为已经绑走。
			if bindErrs[i] != nil {
				// 不是已绑定或修订冲突就停。
				if !errors.Is(bindErrs[i], domain.ErrClientBound) && !errors.Is(bindErrs[i], domain.ErrStaleRevision) {
					// 带上轮号和错误停测。
					t.Fatalf("round %d bind %d: %v", round, i, bindErrs[i])
				}
				continue
			}
			// 已经有一路成了，又成一路就是重复绑定。
			if winner >= 0 {
				// 带上轮号和两份结果停测。
				t.Fatalf("round %d both binds won: %+v %+v", round, bound[winner], bound[i])
			}
			// 记下绑成的那一路。
			winner = i
		}
		// 一路都没成就停，绑定不该全被拒。
		if winner < 0 || bound[winner].BindingRevision != 1 {
			// 带上轮号和全部错误停测。
			t.Fatalf("round %d bind: winner %d %v", round, winner, bindErrs)
		}
		// 改绑目标是三家里除当前所属外的那两家。
		var rebindTo []uuid.UUID
		// 挑出不是当前所属的厂。
		for _, f := range facs {
			// 当前所属不算改分，跳过。
			if f != bindTo[winner] {
				// 收下这家作为改绑目标。
				rebindTo = append(rebindTo, f)
			}
		}
		// 每路改绑结果各占一格，不用加锁。
		moved := make([]store.Client, len(rebindTo))
		// 每路锁内读到的原厂各占一格，不用加锁。
		froms := make([]uuid.UUID, len(rebindTo))
		// 每路改绑错误各占一格，不用加锁。
		moveErrs := make([]error, len(rebindTo))
		// 两路同时把这台设备改到不同的厂。
		runRevisionRace(len(rebindTo), func(i int) {
			// 这一路改到自己的厂，结果、原厂和错误各记一格。
			moved[i], froms[i], moveErrs[i] = s.RebindClient(context.Background(), c.ID, rebindTo[i])
		})
		// 收下成功那几路的修订，后面核对不重号。
		var revs []int64
		// 成功的那几路，后面按修订排好核对原厂接得上。
		var won []int
		// 记下修订最高的那一路，库里应该就是它。
		latest := -1
		// 逐路看结果，成的收下修订，没成的只能是修订冲突。
		for i := range rebindTo {
			// 这一路没成，只能因为修订已被别人升过。
			if moveErrs[i] != nil {
				// 不是修订冲突就停。
				if !errors.Is(moveErrs[i], domain.ErrStaleRevision) {
					// 带上轮号和错误停测。
					t.Fatalf("round %d rebind %d: %v", round, i, moveErrs[i])
				}
				continue
			}
			// 交回的归属不是这一路的目标就停。
			if moved[i].FactoryID == nil || *moved[i].FactoryID != rebindTo[i] {
				// 带上轮号和结果停测。
				t.Fatalf("round %d rebind %d: %+v", round, i, moved[i])
			}
			// 收下这一路的修订。
			revs = append(revs, moved[i].BindingRevision)
			// 这一路成了，参与原厂核对。
			won = append(won, i)
			// 比当前最高还高就换成这一路。
			if latest < 0 || moved[i].BindingRevision > moved[latest].BindingRevision {
				// 记下修订最高的这一路。
				latest = i
			}
		}
		// 一路都没成就停，改绑不该全被拒。
		if latest < 0 {
			// 带上轮号和全部错误停测。
			t.Fatalf("round %d no rebind won: %v", round, moveErrs)
		}
		// 修订从小到大排好，再逐个比对。
		slices.Sort(revs)
		// 成功的修订必须从二往上连续加一，不能重号。
		for i, rev := range revs {
			// 不是二加序号就说明重号或跳号。
			if rev != int64(i)+2 {
				// 带上轮号和全部修订停测。
				t.Fatalf("round %d rebind revisions %v", round, revs)
			}
		}
		// 成功的几路按修订从小到大排，就是真正提交的先后。
		slices.SortFunc(won, func(a, b int) int {
			// 修订小的先提交，排在前面。
			return cmp.Compare(moved[a].BindingRevision, moved[b].BindingRevision)
		})
		// 首个成功那路读到的原厂应是绑定赢家那家。
		want := bindTo[winner]
		// 逐路核对锁内原厂就是前一笔提交后的归属。
		for _, i := range won {
			// 对不上说明交回的不是锁内读到的原厂，作废会发错厂。
			if froms[i] != want {
				// 带上轮号、这一路和应有的原厂停测。
				t.Fatalf("round %d rebind %d from %s, want %s", round, i, froms[i], want)
			}
			// 下一路的原厂应是这一路改到的厂。
			want = rebindTo[i]
		}
		// 读回库里的当前行，核对归属和修订。
		cur, err := s.ClientByID(ctx, c.ID)
		// 读失败、修订不是最高那路、或归属不是它的厂都不对。
		if err != nil || cur.BindingRevision != moved[latest].BindingRevision || cur.FactoryID == nil || *cur.FactoryID != rebindTo[latest] {
			// 带上轮号、当前行和最高那路停测。
			t.Fatalf("round %d final %+v latest %+v err %v", round, cur, moved[latest], err)
		}
	}
}

// runRevisionRace 起 n 路协程，发令后一起跑 fn，全部跑完才返回。
func runRevisionRace(n int, fn func(i int)) {
	// 等所有协程跑完再交回。
	var wg sync.WaitGroup
	// 发令前全部挡住，放开后一起写，尽量撞在一起。
	start := make(chan struct{})
	// 起 n 路，每路带上自己的序号。
	for i := range n {
		// 每路等发令后再跑自己那一份。
		wg.Go(func() {
			// 等发令，免得先起的协程先写完。
			<-start
			// 跑这一路，结果由调用方各记一格。
			fn(i)
		})
	}
	// 一起放开所有协程。
	close(start)
	// 等所有协程跑完。
	wg.Wait()
}
