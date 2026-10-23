package sub

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"eagle/internal/db"
	"eagle/internal/netx"
	"eagle/internal/setting"
)

// Result summarises a subscription update.
type Result struct {
	GID     int      `json:"gid"`
	Added   []string `json:"added"`
	Deleted []string `json:"deleted"`
	// Skipped names the nodes the subscription carried that this build cannot speak,
	// each with the protocol the panel called it by. Empty on an ordinary update.
	Skipped []string `json:"skipped"`
	Message string   `json:"message"`
}

// requestSubscription fetches the address and returns the answer worth importing.
//
// It used to take the first answer that carried the account's quota header and
// stop there, and that stop is how a subscription goes quietly short: a panel
// answers different client identifiers with different formats, and not all of
// them are whole. The panel that came up with this walk had a quieter failure
// than a refusal — it accepted an older Clash spelling, sent the quota header,
// and parked the AnyTLS family outside the YAML — so the only way to know an
// answer is short is to have something whole to compare it against.
//
// So the walk takes the same budget — three attempts — and races them: every
// identifier's request is sent at once, because each one is a full network wait
// (a 10s timeout, longer through a slow panel) and three of those in a row were
// the bulk of every update's silence. The answers are then probed on this
// goroutine in list order — the same dispatch the real parse runs, without the
// store — and the one whose importable-node count is highest wins, the quota
// header breaking a tie (it is what keeps the usage bar and the expiry honest
// when a panel hides it from some formats). The budget is a number, not a
// novelty: shapes are not alternative views of one list — the panel may answer
// each family with a list of a different length, and a different family can
// still be the only whole one — so the walk compares what it is offered, takes
// the best, and says what it took in the log.
//
// The wall the count cannot climb: every shape the panel will answer with may
// be equally short. Nothing in a partial answer says it is partial, and no
// request the client can make says what the other shapes would have carried —
// so the walk compares what it is offered, takes the best, and says what it
// took in the log.
//
// userAgentBudget is how many client identifiers the walk asks at once when it
// is working from the app's own built-in list. Twelve identifiers raced at a
// panel is twelve requests the reader paid for, and the first three cover what
// almost every panel answers.
const userAgentBudget = 3

// readerWroteUserAgents reports whether the identifier list is one the reader
// wrote rather than the app's own built-in one.
//
// The two are told apart by the list itself: the settings form starts out
// holding the built-in list, so an untouched form hands back exactly what
// setting.DefaultUserAgents returns. A list that differs from it in any way is
// the reader's own, and it is walked in full — the field is documented as "the
// order they should be tried", and a reader who pasted a fifth identifier had
// it ignored, so a panel that wanted that fifth answered the walk with nothing
// importable while the identifier that would have worked was never sent.
func readerWroteUserAgents(s *setting.Setting, uas []string) bool {
	if s == nil || strings.TrimSpace(s.UserAgent) == "" {
		return false
	}
	builtin := setting.DefaultUserAgents()
	if len(builtin) != len(uas) {
		return true
	}
	for i := range builtin {
		if builtin[i] != uas[i] {
			return true
		}
	}
	return false
}

// name is for the log only: the address carries the subscription token, and a
// log file is not where that belongs.
func requestSubscription(rawURL, name string, s *setting.Setting, startedID int, logf func(string)) (netx.Response, bool) {
	uas := netx.UserAgents(s)
	// The identifiers race instead of queueing: racing costs the panel no more
	// than the serial walk did, and no answer arrives later than the slowest of
	// them rather than the sum of them. One goroutine per identifier, no shared
	// state beyond the answer slots — HttpGet builds its own client and transport
	// per call, and every goroutine writes only its own slot. The probing, the
	// choosing and the log stay on this goroutine, so the log keeps the order the
	// serial walk had.
	//
	// The budget of three is what the app's OWN list is worth: a dozen
	// identifiers raced at a panel is a dozen requests the reader paid for, and
	// the first three cover what almost every panel answers.
	//
	// A list the reader wrote is not bounded that way. The field is documented
	// as "the order they should be tried", and the walk used to stop at the third
	// of it — so a reader who pasted a fifth identifier never saw it used, and a
	// panel that wanted that fifth answered the walk with nothing importable
	// while the identifier that would have worked was never sent.
	if len(uas) > userAgentBudget && !readerWroteUserAgents(s, uas) {
		uas = uas[:userAgentBudget]
	}
	type fetch struct {
		ua   string
		resp netx.Response
	}
	slots := make([]fetch, len(uas))
	var wg sync.WaitGroup
	for i, ua := range uas {
		wg.Add(1)
		go func(i int, ua string) {
			defer wg.Done()
			slots[i] = fetch{ua: ua, resp: netx.HttpGet(rawURL, s, startedID, ua)}
		}(i, ua)
	}
	wg.Wait()

	var best netx.Response
	bestCount := 0
	var fallback netx.Response
	haveFallback := false
	haveBest := false
	bestHeader := false
	bestKind := ""
	var firstErr string
	requests := 0

	for i, sl := range slots {
		requests++
		resp := sl.resp
		if resp.Error != "" {
			if firstErr == "" {
				firstErr = resp.Error
			}
			continue
		}
		if strings.TrimSpace(resp.Data) == "" {
			// An empty body is not a usable answer, but it is not an error either:
			// the panel answered, it just had nothing to say to this identifier.
			continue
		}
		if !haveFallback {
			fallback, haveFallback = resp, true
		}
		count, kind := probeWith(resp.Data, s)
		header := resp.Header.Get("Subscription-UserInfo") != ""
		logf("<<<<<<<< " + name + ": answer " + strconv.Itoa(i+1) + " read as \"" + kind + "\" with " + strconv.Itoa(count) + " importable node(s)")
		if count > 0 {
			if !haveBest || count > bestCount || (count == bestCount && header && !bestHeader) {
				best, bestCount, bestHeader, bestKind, haveBest = resp, count, header, kind, true
				if i > 0 {
					logf("<<<<<<<< " + name + ": the whole list came from \"" + sl.ua + "\" (answer " + strconv.Itoa(i+1) + ")")
				}
			}
		}
	}

	if haveBest {
		logf("<<<<<<<< " + name + ": the \"" + bestKind + "\" answer is the one to import (" + strconv.Itoa(bestCount) + " node(s))")
		return best, true
	}
	if haveFallback {
		if len(uas) > 1 {
			logf("<<<<<<<< " + name + ": no importable node from " + strconv.Itoa(requests) + " client identifiers; using the first answer as the update's substrate")
		}
		return fallback, true
	}
	if firstErr == "" {
		// Every identifier was answered 2xx with an empty body — the one
		// failure mode with no message of its own. Left as "", the log line
		// above printed nothing and the error the window showed was an empty
		// string: the hardest failure to understand, displayed with no words
		// at all.
		firstErr = "服务器返回了空响应（0 个可解析节点），请更换 User-Agent 或稍后重试"
	}
	return netx.Response{Error: firstErr}, false
}

// Update fetches (when gid identifies a group) and merges a subscription.
//
// live answers the profile id running at the moment it is asked. A walk lasts
// minutes and the reader may switch nodes mid-walk; the deletion phases below
// consult it so a node the core took over after the walk began is not deleted
// out from under it. nil means the caller has no way to ask (tests).
func Update(mgr *db.Manager, s *setting.Setting, startedID int, ref string, gid int, logf func(string), live func() int) (*Result, error) {
	if logf == nil {
		logf = func(string) {}
	}
	// exempt reports whether a profile id is protected from deletion: the id
	// the walk started with, and whatever is running right now. A negative
	// live answer (nothing running — the StoppedID sentinel) protects nothing.
	// Note the >= 0, not > 0: after a wipe the first profile's id IS 0, and a
	// running node with that id used to walk straight out of every protection.
	exempt := func(id int) bool {
		if id == startedID {
			return true
		}
		if live != nil {
			if cur := live(); cur >= 0 && cur == id {
				return true
			}
		}
		return false
	}
	// runningNow is the id the rebuild phases keep by identity: the live
	// answer when there is one, the walk-start id otherwise.
	runningNow := -1
	if live != nil {
		if cur := live(); cur >= 0 {
			runningNow = cur
		}
	}
	if runningNow < 0 && startedID >= 0 {
		runningNow = startedID
	}
	// The group arrives as a deep snapshot: this loop reads and writes its fields
	// over minutes of network waits, and the UI reads the live object meanwhile.
	// All writes land back on the live group through the manager's locked splice.
	group := mgr.SnapshotGroup(gid)
	if group != nil && group.Archive {
		return nil, nil
	}

	content := strings.TrimSpace(ref)
	// The address as handed in, kept before `content` is overwritten with the
	// response body: the name check further down compares the group's name with
	// the label that address would have produced.
	refURL := content
	subUserInfo := ""
	subName := ""
	asURL := gid >= 0

	if asURL {
		groupName := content
		if group != nil {
			groupName = group.Name
		}
		logf(">>>>>>>> Requesting subscription: " + groupName)
		resp, ok := requestSubscription(content, groupName, s, startedID, logf)
		if !ok {
			logf("<<<<<<<< Requesting subscription error: " + resp.Error)
			return nil, fmt.Errorf("%s", resp.Error)
		}
		content = resp.Data
		subUserInfo = resp.Header.Get("Subscription-UserInfo")
		subName = nameFromResponse(resp.Header)
		logf("<<<<<<<< Subscription request finished: " + groupName)
	}

	// The panel's own name for the subscription, applied only while the group is
	// still carrying the placeholder its address produced. Done here rather than
	// at the end so every save below writes the name and the nodes it arrived
	// with in one go — including the "nothing parsed" path, where the name is
	// still the better of the two.
	renamed := false
	if subName != "" && group != nil && isAutoName(group.Name, refURL) {
		logf("<<<<<<<< " + group.Name + " is named \"" + subName + "\" by its subscription")
		group.Name = subName
		renamed = true
	}

	ru := &rawUpdater{s: s, gidAddTo: gid}

	// Snapshot the current members, but change nothing on disk yet. The previous
	// version cleared group.Order, saved it and deleted the members *before*
	// parsing, so any parse failure wiped the whole subscription.
	var in []*db.ProxyEntity
	if group != nil {
		in = groupProfiles(mgr, group.ID)
	}

	ru.update(content)
	// One lock, one pass: the whole parse batch lands in the manager here rather
	// than one entity per queue (see AddProfiles).
	if err := mgr.AddProfiles(ru.updated, gid); err != nil {
		return nil, err
	}

	res := &Result{GID: gid}
	if group == nil {
		return res, nil
	}

	// Nothing parsed. The request itself may well have succeeded — subscription
	// panels answer an unrecognised User-Agent with an empty body or an HTML
	// notice, and the traffic header still arrives — but there is nothing to
	// show for it. Keep the existing members and the existing order, and report
	// the likely cause instead of silently emptying the subscription.
	if len(ru.updated) == 0 {
		// Only the fields this update owns go back: a whole-snapshot splice
		// (SpliceGroup) would roll the name/URL/order the reader may have been
		// editing during the minutes the network waits took. The Order is left
		// alone on this path — nothing parsed means the existing members and
		// order stay exactly as the live group has them.
		_ = mgr.UpdateGroup(gid, true, func(g *db.Group) {
			g.SubLastUpdate = time.Now().Unix()
			g.Info = subUserInfo
			if renamed {
				g.Name = group.Name
			}
		})
		logf("<<<<<<<< " + group.Name + ": subscription returned no usable node (" + strconv.Itoa(len(content)) + " bytes) — nodes left untouched")
		// The distinction that matters here: a body that carried nothing, and a body
		// whose every node was in a protocol this build does not speak. The second is
		// not a User-Agent problem at all, and naming the protocols is the whole answer.
		if len(ru.skipped) > 0 {
			return nil, fmt.Errorf("订阅里的 %d 个节点本程序都不支持，已全部跳过：%s",
				len(ru.skipped), strings.Join(ru.skipped, "、"))
		}
		return nil, fmt.Errorf(
			"订阅未解析出任何节点（响应 %d 字节）。服务器可能不接受当前 User-Agent「%s」，请更换后重试；原有节点已保留",
			len(content), netx.UserAgent(s))
	}

	group.SubLastUpdate = time.Now().Unix()
	group.Info = subUserInfo

	if s.SubClear {
		// Done after the parse so a failed one cannot delete anything. The new
		// profiles already have fresh ids, so removing the snapshot cannot touch
		// them.
		logf("Clearing servers...")
		for _, p := range in {
			// Except the one the core is running. "Clear" is about the list the reader
			// wants rebuilt, not about pulling the node out from under the tunnel they
			// are using: deleting it left the running id naming nothing, which is the
			// state every restart path answers with "profile N not found" (see the note
			// in the merge branch below). The live answer covers a node the reader
			// switched to while this walk ran.
			if exempt(p.ID) {
				continue
			}
			// The clear's deletion count is the one this branch used to drop: the
			// report below carried Added only, so a "clear on update" — the mode
			// whose deletions are by far the largest — said nothing about what it
			// removed, and the toast and the change log both read as though only
			// additions had happened.
			res.Deleted = append(res.Deleted, p.DisplayType()+" "+p.DisplayName())
			_ = mgr.DeleteProfile(p.ID)
		}
	}

	outAll := groupProfiles(mgr, group.ID)
	var change string

	if s.SubClear {
		// Every member was just recreated, so the order is simply what the parse
		// produced. The original code never refilled the order on this path: it
		// was cleared before the parse and left empty, which is why a
		// "clear on update" subscription always showed zero nodes.
		group.Order = make([]int, 0, len(outAll)+1)
		// The node the core is running leads the list, and it is kept by IDENTITY
		// rather than by letting its fresh id in: deleting it leaves the running id
		// naming nothing (every restart path then answers "profile N not found"), and
		// admitting both would list one node twice. The clear is about the list the
		// reader wants rebuilt, not about the node they are using — that one keeps its
		// id and the measurements taken on it, and the next update takes it out like
		// any other node, once nothing is running from it.
		var keep *db.ProxyEntity
		if runningNow >= 0 {
			if keep = mgr.SnapshotProfile(runningNow); keep != nil && keep.GID != group.ID {
				// The running id is global, this walk is one group's: a node of
				// another group keeps its id but stays out of this order —
				// putting it here would render it in two groups at once, and a
				// same-content node of THIS group would then be deleted as its
				// "twin".
				keep = nil
			}
		}
		// The content of every node the clear spared. A fresh node with the same
		// content is its twin, and the twin is the one that goes: the spared node
		// keeps its id and what the reader measured on it.
		keepKeys := map[string]bool{}
		if keep != nil {
			group.Order = append(group.Order, keep.ID)
			keepKeys[BeanKey(keep, false)] = true
			logf("<<<<<<<< 运行中的节点在清空后保留: " + keep.DisplayName())
		}
		// The other node the clear spared: the one the walk started on. It is on
		// disk still, and it has to be in the order as well — a node that is on
		// disk but not in the order is a leftover the reader can neither see nor
		// delete, and the next update takes it out like any other node.
		for _, p := range in {
			if !exempt(p.ID) || p.GID != group.ID || containsInt(group.Order, p.ID) {
				continue
			}
			group.Order = append(group.Order, p.ID)
			keepKeys[BeanKey(p, false)] = true
		}
		// Twins are collected, not deleted here: the order below is the write the
		// deletions depend on, and a delete that lands before a failed order save
		// leaves the old order naming files that are gone (the next start then
		// reads an empty group). They go once the order is on disk.
		var twins []int
		for _, e := range outAll {
			// outAll is a query of what the group holds now, so the kept nodes are
			// in it; the exempt ones are already in the order above (or belong to
			// another group and must not enter this one).
			if exempt(e.ID) {
				continue
			}
			if keepKeys[BeanKey(e, false)] {
				// Its fresh twin: the node is already listed above, with what the
				// reader measured on it.
				twins = append(twins, e.ID)
				continue
			}
			group.Order = append(group.Order, e.ID)
			res.Added = append(res.Added, e.DisplayType()+" "+e.DisplayName())
		}
		// Same rule as the merge branch below: the order has to be on disk
		// before anything is deleted on the strength of it. A failure here is
		// not fatal to the parse (the nodes are imported), but the report has to
		// carry it rather than the next start discovering an order that names
		// files which are gone.
		if err := mgr.UpdateGroup(gid, true, func(g *db.Group) {
			g.Order = group.Order
			g.SubLastUpdate = time.Now().Unix()
			g.Info = subUserInfo
			if renamed {
				g.Name = group.Name
			}
		}); err != nil {
			return nil, fmt.Errorf("保存分组顺序失败: %w", err)
		}
		for _, id := range twins {
			_ = mgr.DeleteProfile(id)
		}
	} else {
		// Which parsed node each existing node is a re-statement of, matched by
		// content (BeanKey — the bean with its empty fields dropped and the two
		// cached-config keys ignored) rather than by pointer. The parse hands back
		// freshly built entities while `in` is a deep copy of the old ones, so
		// pointer identity could never hold between the two — which is how every
		// update came to rebuild the whole subscription: a new id for every node,
		// the running one included, its measurements zeroed, the remembered id
		// left dangling and the supervisor's next restart hunting a node that had
		// just been deleted. Matching by content is what the ids are supposed to
		// survive.
		//
		// A match keeps the OLD entity: the id, the latency, the speed and the
		// traffic all stay with the node the reader measured. The fresh twin the
		// parse added is the one that goes.
		byKey := map[string]*db.ProxyEntity{}
		for _, p := range in {
			if k := BeanKey(p, false); byKey[k] == nil {
				byKey[k] = p
			}
		}
		reused := map[int]bool{}
		group.Order = make([]int, 0, len(ru.updated))
		for _, ent := range ru.updated {
			if old := byKey[BeanKey(ent, false)]; old != nil && !reused[old.ID] {
				reused[old.ID] = true
				group.Order = append(group.Order, old.ID)
				// A duplicate from here on: same content, same group, and an id
				// nothing has been told about yet.
				_ = mgr.DeleteProfile(ent.ID)
				continue
			}
			group.Order = append(group.Order, ent.ID)
			res.Added = append(res.Added, ent.DisplayType()+" "+ent.DisplayName())
		}
		// The node the core is running is never taken out by an update, and it goes
		// back into the order so it stays visible in its group. A subscription that
		// no longer carries it — or carries it with a changed credential, which the
		// content key above reads as a different node — used to delete its id while
		// the core went on serving it: the id then named nothing anywhere, and every
		// path that names the running node answered "profile N not found" — the mode
		// switch that restarts it (TUN ↔ system proxy, which is where the reader met
		// it), the supervisor restarting it after a crash, 重启节点, the status bar.
		// The next update takes it out, once nothing is running from it.
		// The >= 0, not > 0: after a wipe the first profile's id IS 0.
		if startedID >= 0 && !containsInt(group.Order, startedID) {
			for _, p := range in {
				if p.ID != startedID {
					continue
				}
				// The id is global, this walk is one group's: a node the reader moved to
				// another group keeps its id, and admitting it here rendered it in both
				// groups at once — the clean-up then kept it in this one too, because
				// the order held it. The clear branch above has the same guard.
				if p.GID != group.ID {
					break
				}
				group.Order = append(group.Order, startedID)
				logf("<<<<<<<< 运行中的节点已不在订阅里，本次保留: " + p.DisplayName())
				break
			}
		}
		// A node the reader switched to while this walk ran deserves the same
		// protection the walk-start running id has: if it is this group's and
		// the subscription no longer carries it, it goes back into the order —
		// the cleanup below deletes everything the order does not hold.
		if live != nil {
			if cur := live(); cur >= 0 && !containsInt(group.Order, cur) {
				if p := mgr.SnapshotProfile(cur); p != nil && p.GID == gid {
					group.Order = append(group.Order, cur)
					logf("<<<<<<<< 走中切换到的运行节点已不在订阅里，本次保留: " + p.DisplayName())
				}
			}
		}
		for _, p := range in {
			// Anything the final order still holds is kept, whatever protected it
			// (content match, the running id, a mid-walk switch) — the toast
			// counts only what really went.
			if !containsInt(group.Order, p.ID) {
				res.Deleted = append(res.Deleted, p.DisplayType()+" "+p.DisplayName())
			}
		}
		// The order is the last write that has to land before the cleanup: it is
		// what says which nodes are still in the subscription, and the deletions
		// below act on it. Swallowing this error meant the disk kept the old
		// order while the removed nodes' files went anyway — the next start read
		// an order naming files that were no longer there, and the whole group
		// came up empty with the nodes only findable through the cross-group
		// search.
		if err := mgr.UpdateGroup(gid, true, func(g *db.Group) {
			g.Order = group.Order
			g.SubLastUpdate = time.Now().Unix()
			g.Info = subUserInfo
			if renamed {
				g.Name = group.Name
			}
		}); err != nil {
			return nil, fmt.Errorf("保存分组顺序失败，本次未删除任何节点: %w", err)
		}

		// cleanup profiles no longer referenced
		for _, ent := range outAll {
			if !containsInt(group.Order, ent.ID) && !exempt(ent.ID) {
				_ = mgr.DeleteProfile(ent.ID)
			}
		}

	}

	// One report shape for both branches, assembled from res rather than built
	// inside the merge only: the clear branch filled Added and (since the fix)
	// Deleted too, and a reader seeing two formats for one toast — or, before,
	// no deletion half at all on the branch whose deletions are the largest —
	// had to guess what the update did.
	change = fmt.Sprintf("Added %d profiles:\n%s\nDeleted %d profiles:\n%s",
		len(res.Added), joinLines(res.Added), len(res.Deleted), joinLines(res.Deleted))
	if len(res.Added)+len(res.Deleted) == 0 {
		change = "Nothing"
	}

	// What did not come across, said once at the end. A protocol this build cannot
	// speak is not an update failure — but it is why a subscription's node count never
	// matches the panel's, and staying silent about it is what turns that into a
	// mystery.
	res.Skipped = ru.skipped
	if len(res.Skipped) > 0 {
		change += fmt.Sprintf("\nSkipped %d unsupported:\n%s", len(res.Skipped), joinLines(res.Skipped))
		logf("<<<<<<<< " + group.Name + ": " + strconv.Itoa(len(res.Skipped)) + " node(s) skipped (protocol not supported)")
	}

	logf("<<<<<<<< Change of " + group.Name + ":\n" + change)
	res.Message = change
	return res, nil
}

// UpdateAll updates every group that has a subscription URL.
//
// Every one of them: the group-level "skip auto update" flag this used to honour is
// gone. It existed for the background timers, and what it answered — whether this
// group is fetched on a clock — is already said by the interval being empty (see
// subUpdateLoop).
func UpdateAll(mgr *db.Manager, s *setting.Setting, startedID int, logf func(string), live func() int) []error {
	var errs []error
	for _, gid := range mgr.TabOrder() {
		g := mgr.SnapshotGroup(gid)
		if g == nil || g.URL == "" || g.Archive {
			continue
		}
		if _, err := Update(mgr, s, startedID, g.URL, gid, logf, live); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", g.Name, err))
		}
	}
	return errs
}

func groupProfiles(mgr *db.Manager, gid int) []*db.ProxyEntity {
	var out []*db.ProxyEntity
	for _, p := range mgr.AllProfiles() {
		if p.GID == gid {
			out = append(out, p)
		}
	}
	return out
}

func indexOfPointer(list []*db.ProxyEntity, target *db.ProxyEntity) int {
	for i, e := range list {
		if e == target {
			return i
		}
	}
	return -1
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func joinLines(items []string) string {
	return strings.Join(items, "\n")
}
