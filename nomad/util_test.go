package nomad

import (
	"net"
	"reflect"
	"testing"
	"time"

	version "github.com/hashicorp/go-version"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/serf/serf"
	"github.com/stretchr/testify/require"
)

func TestIsNomadServer(t *testing.T) {
	t.Parallel()
	m := serf.Member{
		Name:   "foo",
		Addr:   net.IP([]byte{127, 0, 0, 1}),
		Status: serf.StatusAlive,
		Tags: map[string]string{
			"role":     "nomad",
			"region":   "aws",
			"dc":       "east-aws",
			"rpc_addr": "1.1.1.1",
			"port":     "10000",
			"vsn":      "1",
			"raft_vsn": "2",
			"build":    "0.7.0+ent",
			"nonvoter": "1",
		},
	}
	valid, parts := isNomadServer(m)
	if !valid || parts.Region != "aws" ||
		parts.Datacenter != "east-aws" || parts.Port != 10000 {
		t.Fatalf("bad: %v %v", valid, parts)
	}
	if parts.Name != "foo" {
		t.Fatalf("bad: %v", parts)
	}
	if parts.Bootstrap {
		t.Fatalf("unexpected bootstrap")
	}
	if parts.Expect != 0 {
		t.Fatalf("bad: %v", parts.Expect)
	}
	if parts.Status != serf.StatusAlive {
		t.Fatalf("bad: %v", parts.Status)
	}
	if parts.RaftVersion != 2 {
		t.Fatalf("bad: %v", parts.RaftVersion)
	}
	if parts.RPCAddr.String() != "1.1.1.1:10000" {
		t.Fatalf("bad: %v", parts.RPCAddr.String())
	}
	if seg := parts.Build.Segments(); len(seg) != 3 {
		t.Fatalf("bad: %v", parts.Build)
	} else if seg[0] != 0 && seg[1] != 7 && seg[2] != 0 {
		t.Fatalf("bad: %v", parts.Build)
	}
	if !parts.NonVoter {
		t.Fatalf("should be nonvoter")
	}

	m.Tags["bootstrap"] = "1"
	valid, parts = isNomadServer(m)
	if !valid || !parts.Bootstrap {
		t.Fatalf("expected bootstrap")
	}
	if parts.Addr.String() != "127.0.0.1:10000" {
		t.Fatalf("bad addr: %v", parts.Addr)
	}
	if parts.MajorVersion != 1 {
		t.Fatalf("bad: %v", parts)
	}

	m.Tags["expect"] = "3"
	delete(m.Tags, "bootstrap")
	valid, parts = isNomadServer(m)
	if !valid || parts.Expect != 3 {
		t.Fatalf("bad: %v", parts.Expect)
	}

	delete(m.Tags, "nonvoter")
	valid, parts = isNomadServer(m)
	if !valid || parts.NonVoter {
		t.Fatalf("should be a voter")
	}
}

func TestServersMeetMinimumVersionExcludingFailed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		members  []serf.Member
		ver      *version.Version
		expected bool
	}{
		// One server, meets reqs
		{
			members: []serf.Member{
				makeMember("0.7.5", serf.StatusAlive),
			},
			ver:      version.Must(version.NewVersion("0.7.5")),
			expected: true,
		},
		// One server in dev, meets reqs
		{
			members: []serf.Member{
				makeMember("0.8.5-dev", serf.StatusAlive),
			},
			ver:      version.Must(version.NewVersion("0.7.5")),
			expected: true,
		},
		// One server with meta, meets reqs
		{
			members: []serf.Member{
				makeMember("0.7.5+ent", serf.StatusAlive),
			},
			ver:      version.Must(version.NewVersion("0.7.5")),
			expected: true,
		},
		// One server, doesn't meet reqs
		{
			members: []serf.Member{
				makeMember("0.7.5", serf.StatusAlive),
			},
			ver:      version.Must(version.NewVersion("0.8.0")),
			expected: false,
		},
		// Multiple servers, meets req version, includes failed that doesn't meet req
		{
			members: []serf.Member{
				makeMember("0.7.5", serf.StatusAlive),
				makeMember("0.8.0", serf.StatusAlive),
				makeMember("0.7.0", serf.StatusFailed),
			},
			ver:      version.Must(version.NewVersion("0.7.5")),
			expected: true,
		},
		// Multiple servers, doesn't meet req version
		{
			members: []serf.Member{
				makeMember("0.7.5", serf.StatusAlive),
				makeMember("0.8.0", serf.StatusAlive),
			},
			ver:      version.Must(version.NewVersion("0.8.0")),
			expected: false,
		},
	}

	for _, tc := range cases {
		result := ServersMeetMinimumVersion(tc.members, tc.ver, false)
		if result != tc.expected {
			t.Fatalf("bad: %v, %v, %v", result, tc.ver.String(), tc)
		}
	}
}

func TestServersMeetMinimumVersionIncludingFailed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		members  []serf.Member
		ver      *version.Version
		expected bool
	}{
		// Multiple servers, meets req version
		{
			members: []serf.Member{
				makeMember("0.7.5", serf.StatusAlive),
				makeMember("0.8.0", serf.StatusAlive),
				makeMember("0.7.5", serf.StatusFailed),
			},
			ver:      version.Must(version.NewVersion("0.7.5")),
			expected: true,
		},
		// Multiple servers, doesn't meet req version
		{
			members: []serf.Member{
				makeMember("0.7.5", serf.StatusAlive),
				makeMember("0.8.0", serf.StatusAlive),
				makeMember("0.7.0", serf.StatusFailed),
			},
			ver:      version.Must(version.NewVersion("0.7.5")),
			expected: false,
		},
	}

	for _, tc := range cases {
		result := ServersMeetMinimumVersion(tc.members, tc.ver, true)
		if result != tc.expected {
			t.Fatalf("bad: %v, %v, %v", result, tc.ver.String(), tc)
		}
	}
}

func makeMember(version string, status serf.MemberStatus) serf.Member {
	return serf.Member{
		Name: "foo",
		Addr: net.IP([]byte{127, 0, 0, 1}),
		Tags: map[string]string{
			"role":   "nomad",
			"region": "aws",
			"dc":     "east-aws",
			"port":   "10000",
			"build":  version,
			"vsn":    "1",
		},
		Status: status,
	}
}

func TestShuffleStrings(t *testing.T) {
	t.Parallel()
	// Generate input
	inp := make([]string, 10)
	for idx := range inp {
		inp[idx] = uuid.Generate()
	}

	// Copy the input
	orig := make([]string, len(inp))
	copy(orig, inp)

	// Shuffle
	shuffleStrings(inp)

	// Ensure order is not the same
	if reflect.DeepEqual(inp, orig) {
		t.Fatalf("shuffle failed")
	}
}

func Test_partitionAll(t *testing.T) {
	xs := []string{"a", "b", "c", "d", "e", "f"}
	// evenly divisible
	require.Equal(t, [][]string{{"a", "b"}, {"c", "d"}, {"e", "f"}}, partitionAll(2, xs))
	require.Equal(t, [][]string{{"a", "b", "c"}, {"d", "e", "f"}}, partitionAll(3, xs))
	// whole thing fits int the last part
	require.Equal(t, [][]string{{"a", "b", "c", "d", "e", "f"}}, partitionAll(7, xs))
	// odd remainder
	require.Equal(t, [][]string{{"a", "b", "c", "d"}, {"e", "f"}}, partitionAll(4, xs))
	// zero size
	require.Equal(t, [][]string{{"a", "b", "c", "d", "e", "f"}}, partitionAll(0, xs))
	// one size
	require.Equal(t, [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}}, partitionAll(1, xs))
}

func TestMaxUint64(t *testing.T) {
	t.Parallel()
	if maxUint64(1, 2) != 2 {
		t.Fatalf("bad")
	}
	if maxUint64(2, 2) != 2 {
		t.Fatalf("bad")
	}
	if maxUint64(2, 1) != 2 {
		t.Fatalf("bad")
	}
}

func TestDropButLastChannelDropsValues(t *testing.T) {
	sourceCh := make(chan bool)
	shutdownCh := make(chan struct{})
	defer close(shutdownCh)

	dstCh := dropButLastChannel(sourceCh, shutdownCh)

	// timeout duration for any channel propagation delay
	timeoutDuration := 5 * time.Millisecond

	// test that dstCh doesn't emit anything initially
	select {
	case <-dstCh:
		require.Fail(t, "received a message unexpectedly")
	case <-time.After(timeoutDuration):
		// yay no message - it could have been a default: but
		// checking for goroutine effect
	}

	sourceCh <- false
	select {
	case v := <-dstCh:
		require.False(t, v, "unexpected value from dstCh Ch")
	case <-time.After(timeoutDuration):
		require.Fail(t, "timed out waiting for source->dstCh propagation")
	}

	// channel is drained now
	select {
	case v := <-dstCh:
		require.Failf(t, "received a message unexpectedly", "value: %v", v)
	case <-time.After(timeoutDuration):
		// yay no message - it could have been a default: but
		// checking for goroutine effect
	}

	// now enqueue many messages and ensure only last one is received
	// enqueueing should be fast!
	sourceCh <- false
	sourceCh <- false
	sourceCh <- false
	sourceCh <- false
	sourceCh <- true

	// I suspect that dstCh may contain a stale (i.e. `false`) value if golang executes
	// this select before the implementation goroutine dequeues last value.
	//
	// However, never got it to fail in test - so leaving it now to see if it ever fails;
	// and if/when test fails, we can learn of how much of an issue it is and adjust
	select {
	case v := <-dstCh:
		require.True(t, v, "unexpected value from dstCh Ch")
	case <-time.After(timeoutDuration):
		require.Fail(t, "timed out waiting for source->dstCh propagation")
	}

	sourceCh <- true
	sourceCh <- true
	sourceCh <- true
	sourceCh <- true
	sourceCh <- true
	sourceCh <- false
	select {
	case v := <-dstCh:
		require.False(t, v, "unexpected value from dstCh Ch")
	case <-time.After(timeoutDuration):
		require.Fail(t, "timed out waiting for source->dstCh propagation")
	}
}

// TestDropButLastChannel_DeliversMessages asserts that last
// message is always delivered, some messages are dropped but never
// introduce new messages.
// On tight loop, receivers may get some intermediary messages.
func TestDropButLastChannel_DeliversMessages(t *testing.T) {
	sourceCh := make(chan bool)
	shutdownCh := make(chan struct{})
	defer close(shutdownCh)

	dstCh := dropButLastChannel(sourceCh, shutdownCh)

	// timeout duration for any channel propagation delay
	timeoutDuration := 5 * time.Millisecond

	sentMessages := 100
	go func() {
		for i := 0; i < sentMessages-1; i++ {
			sourceCh <- true
		}
		sourceCh <- false
	}()

	receivedTrue, receivedFalse := 0, 0
	var lastReceived *bool

RECEIVE_LOOP:
	for {
		select {
		case v := <-dstCh:
			lastReceived = &v
			if v {
				receivedTrue++
			} else {
				receivedFalse++
			}

		case <-time.After(timeoutDuration):
			break RECEIVE_LOOP
		}
	}

	t.Logf("receiver got %v out %v true messages, and %v out of %v false messages",
		receivedTrue, sentMessages-1, receivedFalse, 1)

	require.NotNil(t, lastReceived)
	require.False(t, *lastReceived)
	require.Equal(t, 1, receivedFalse)
	require.LessOrEqual(t, receivedTrue, sentMessages-1)
}

// TestDropButLastChannel_DeliversMessages_Close asserts that last
// message is always delivered, some messages are dropped but never
// introduce new messages, even with a closed signal.
func TestDropButLastChannel_DeliversMessages_Close(t *testing.T) {
	sourceCh := make(chan bool)
	shutdownCh := make(chan struct{})
	defer close(shutdownCh)

	dstCh := dropButLastChannel(sourceCh, shutdownCh)

	// timeout duration for any channel propagation delay
	timeoutDuration := 5 * time.Millisecond

	sentMessages := 100
	go func() {
		for i := 0; i < sentMessages-1; i++ {
			sourceCh <- true
		}
		sourceCh <- false
		close(sourceCh)
	}()

	receivedTrue, receivedFalse := 0, 0
	var lastReceived *bool

RECEIVE_LOOP:
	for {
		select {
		case v, ok := <-dstCh:
			if !ok {
				break RECEIVE_LOOP
			}
			lastReceived = &v
			if v {
				receivedTrue++
			} else {
				receivedFalse++
			}

		case <-time.After(timeoutDuration):
			require.Fail(t, "timed out while waiting for messages")
		}
	}

	t.Logf("receiver got %v out %v true messages, and %v out of %v false messages",
		receivedTrue, sentMessages-1, receivedFalse, 1)

	require.NotNil(t, lastReceived)
	require.False(t, *lastReceived)
	require.Equal(t, 1, receivedFalse)
	require.LessOrEqual(t, receivedTrue, sentMessages-1)
}
