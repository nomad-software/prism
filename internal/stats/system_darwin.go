package stats

/*
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <libproc.h>
#include <mach/mach.h>
#include <mach/mach_time.h>
#include <mach/mach_host.h>
#include <mach/processor_info.h>
#include <sys/param.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/sysctl.h>
#include <sys/resource.h>
#include <net/if.h>
#include <net/if_dl.h>
#include <net/route.h>

// cpu_ticks writes the ticks of up to max processors into out, four to each:
// user, system, idle and nice, as CPU_STATE_* number them. It returns how
// many processors there are, or -1 if they could not be read.
static int cpu_ticks(unsigned long long *out, int max) {
	natural_t count;
	processor_info_array_t info;
	mach_msg_type_number_t size;
	if (host_processor_info(mach_host_self(), PROCESSOR_CPU_LOAD_INFO, &count, &info, &size) != KERN_SUCCESS) {
		return -1;
	}
	for (int i = 0; i < (int)count && i < max; i++) {
		for (int j = 0; j < CPU_STATE_MAX; j++) {
			out[i * CPU_STATE_MAX + j] = (unsigned int)info[i * CPU_STATE_MAX + j];
		}
	}
	vm_deallocate(mach_task_self(), (vm_address_t)info, size * sizeof(integer_t));
	return (int)count;
}

// memory writes the size of memory, how much of it is used, and the size and
// use of swap, in bytes. Memory in use is counted as Activity Monitor counts
// it: what programs use and cannot be purged, what is wired, and what is
// compressed. It returns 0, or -1 if they could not be read.
static int memory(unsigned long long *total, unsigned long long *used,
		unsigned long long *swap_total, unsigned long long *swap_used) {
	size_t len = sizeof(*total);
	if (sysctlbyname("hw.memsize", total, &len, NULL, 0) != 0) {
		return -1;
	}
	vm_size_t page;
	if (host_page_size(mach_host_self(), &page) != KERN_SUCCESS) {
		return -1;
	}
	vm_statistics64_data_t vm;
	mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
	if (host_statistics64(mach_host_self(), HOST_VM_INFO64, (host_info64_t)&vm, &count) != KERN_SUCCESS) {
		return -1;
	}
	unsigned long long app = vm.internal_page_count > vm.purgeable_count ?
		vm.internal_page_count - vm.purgeable_count : 0;
	*used = (app + vm.wire_count + vm.compressor_page_count) * (unsigned long long)page;
	if (*used > *total) {
		*used = *total;
	}
	struct xsw_usage swap;
	len = sizeof(swap);
	if (sysctlbyname("vm.swapusage", &swap, &len, NULL, 0) != 0) {
		return -1;
	}
	*swap_total = swap.xsu_total;
	*swap_used = swap.xsu_used;
	return 0;
}

// network writes the names of up to max network adapters into names, each
// IFNAMSIZ long, and their bytes received and sent into rx and tx. Their 64
// bit counters are in the messages NET_RT_IFLIST2 gives; those getifaddrs
// gives are 32 bits, and start again after 4 GiB. It returns how many were
// written, or -1 if they could not be read.
static int network(char *names, unsigned long long *rx, unsigned long long *tx, int max) {
	int mib[6] = {CTL_NET, PF_ROUTE, 0, 0, NET_RT_IFLIST2, 0};
	size_t len;
	if (sysctl(mib, 6, NULL, &len, NULL, 0) != 0) {
		return -1;
	}
	char *buf = malloc(len);
	if (buf == NULL) {
		return -1;
	}
	if (sysctl(mib, 6, buf, &len, NULL, 0) != 0) {
		free(buf);
		return -1;
	}
	int n = 0;
	for (char *next = buf; next < buf + len && n < max;) {
		struct if_msghdr *ifm = (struct if_msghdr *)next;
		if (ifm->ifm_msglen == 0) {
			break;
		}
		next += ifm->ifm_msglen;
		if (ifm->ifm_type != RTM_IFINFO2) {
			continue;
		}
		struct if_msghdr2 *if2 = (struct if_msghdr2 *)ifm;
		struct sockaddr_dl *sdl = (struct sockaddr_dl *)(if2 + 1);
		int nlen = sdl->sdl_nlen < IFNAMSIZ - 1 ? sdl->sdl_nlen : IFNAMSIZ - 1;
		memset(names + n * IFNAMSIZ, 0, IFNAMSIZ);
		memcpy(names + n * IFNAMSIZ, sdl->sdl_data, nlen);
		rx[n] = if2->ifm_data.ifi_ibytes;
		tx[n] = if2->ifm_data.ifi_obytes;
		n++;
	}
	free(buf);
	return n;
}

// process is what is read of a process.
typedef struct {
	int pid, ppid;
	unsigned int uid;
	int restricted;
	unsigned long long start, cpu, memory;
	char name[2 * MAXCOMLEN + 1];
} process;

// start_of returns when the process with the information started, in
// microseconds since 1970.
static unsigned long long start_of(struct kinfo_proc *kp) {
	return (unsigned long long)kp->kp_proc.p_starttime.tv_sec * 1000000 + kp->kp_proc.p_starttime.tv_usec;
}

// processes returns every process, in memory the caller frees, and writes
// how many there are into n; or it returns NULL if they could not be read.
// The time and memory of each come from proc_pid_rusage, which reads those
// of other users' processes only for root; a process it fails for is
// restricted. Its times are in Mach's units, which are made nanoseconds.
static process *processes(int *n) {
	int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_ALL, 0};
	struct kinfo_proc *kp = NULL;
	size_t len;
	// Processes can start between asking how much room they take and
	// reading them, so they are read with room to spare, and again if that
	// is not enough.
	for (int tries = 0;; tries++) {
		if (sysctl(mib, 4, NULL, &len, NULL, 0) != 0) {
			return NULL;
		}
		len += len / 8 + 16 * sizeof(struct kinfo_proc);
		kp = malloc(len);
		if (kp == NULL) {
			return NULL;
		}
		if (sysctl(mib, 4, kp, &len, NULL, 0) == 0) {
			break;
		}
		free(kp);
		if (errno != ENOMEM || tries == 3) {
			return NULL;
		}
	}
	int count = (int)(len / sizeof(struct kinfo_proc));
	process *out = calloc(count > 0 ? count : 1, sizeof(process));
	if (out == NULL) {
		free(kp);
		return NULL;
	}
	mach_timebase_info_data_t tb;
	if (mach_timebase_info(&tb) != KERN_SUCCESS || tb.denom == 0) {
		tb.numer = tb.denom = 1;
	}
	for (int i = 0; i < count; i++) {
		process *p = &out[i];
		p->pid = kp[i].kp_proc.p_pid;
		p->ppid = kp[i].kp_eproc.e_ppid;
		p->uid = kp[i].kp_eproc.e_pcred.p_ruid;
		p->start = start_of(&kp[i]);
		// proc_name gives a name twice as long as p_comm has room for.
		if (proc_name(p->pid, p->name, sizeof(p->name)) <= 0) {
			strlcpy(p->name, kp[i].kp_proc.p_comm, sizeof(p->name));
		}
		struct rusage_info_v2 ri;
		if (proc_pid_rusage(p->pid, RUSAGE_INFO_V2, (rusage_info_t *)&ri) == 0) {
			p->cpu = (ri.ri_user_time + ri.ri_system_time) * tb.numer / tb.denom;
			p->memory = ri.ri_phys_footprint;
		} else {
			p->restricted = 1;
		}
	}
	free(kp);
	*n = count;
	return out;
}

// process_start writes when the process with the PID started into start. It
// returns 0, or -1 if there is no such process.
static int process_start(int pid, unsigned long long *start) {
	int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PID, pid};
	struct kinfo_proc kp;
	size_t len = sizeof(kp);
	if (sysctl(mib, 4, &kp, &len, NULL, 0) != 0 || len == 0) {
		return -1;
	}
	*start = start_of(&kp);
	return 0;
}
*/
import "C"

import (
	"errors"
	"time"
	"unsafe"
)

// firstUserUID is the first user ID macOS gives people.
const firstUserUID = 501

// darwin reads the statistics from the Mach kernel and sysctl.
type darwin struct{}

func newSystem() Source {
	return darwin{}
}

// maxCPUs and maxAdapters are the most processors and network adapters that
// are read; any beyond them are left out.
const (
	maxCPUs     = 1024
	maxAdapters = 256
)

// CPU reads the ticks of each processor, and adds them up for all of them,
// as macOS keeps no total of its own.
func (darwin) CPU() (CPU, error) {
	const states = C.CPU_STATE_MAX
	ticks := make([]C.ulonglong, maxCPUs*states)
	n := int(C.cpu_ticks(&ticks[0], maxCPUs))
	if n < 0 {
		return CPU{}, errors.New("the processors' times could not be read")
	}
	n = min(n, maxCPUs)
	c := CPU{Cores: make([]CPUTimes, n)}
	for i := range n {
		t := ticks[i*states : (i+1)*states]
		core := CPUTimes{
			Busy: uint64(t[C.CPU_STATE_USER] + t[C.CPU_STATE_SYSTEM] + t[C.CPU_STATE_NICE]),
			Idle: uint64(t[C.CPU_STATE_IDLE]),
		}
		c.Cores[i] = core
		c.All.Busy += core.Busy
		c.All.Idle += core.Idle
	}
	return c, nil
}

func (darwin) Memory() (Memory, error) {
	var total, used, swapTotal, swapUsed C.ulonglong
	if C.memory(&total, &used, &swapTotal, &swapUsed) != 0 {
		return Memory{}, errors.New("the use of memory could not be read")
	}
	return Memory{
		Total:     uint64(total),
		Used:      uint64(used),
		SwapTotal: uint64(swapTotal),
		SwapUsed:  uint64(swapUsed),
	}, nil
}

func (darwin) Network() (map[string]NetCounters, error) {
	names := make([]C.char, maxAdapters*C.IFNAMSIZ)
	rx := make([]C.ulonglong, maxAdapters)
	tx := make([]C.ulonglong, maxAdapters)
	n := int(C.network(&names[0], &rx[0], &tx[0], maxAdapters))
	if n < 0 {
		return nil, errors.New("the network adapters could not be read")
	}
	counters := make(map[string]NetCounters, n)
	for i := range n {
		name := C.GoString((*C.char)(unsafe.Pointer(&names[i*C.IFNAMSIZ])))
		// An adapter can have more than one message; the counters are the
		// same in each.
		counters[name] = NetCounters{Received: uint64(rx[i]), Sent: uint64(tx[i])}
	}
	return counters, nil
}

// Processes reads every process, from sysctl, with the time and memory of
// each that proc_pid_rusage can read.
func (darwin) Processes() ([]Process, error) {
	var n C.int
	list := C.processes(&n)
	if list == nil {
		return nil, errors.New("the processes could not be read")
	}
	defer C.free(unsafe.Pointer(list))
	processes := make([]Process, 0, int(n))
	for _, p := range unsafe.Slice(list, int(n)) {
		processes = append(processes, Process{
			PID:        int(p.pid),
			PPID:       int(p.ppid),
			UID:        int(p.uid),
			Name:       C.GoString(&p.name[0]),
			Start:      uint64(p.start),
			CPU:        time.Duration(p.cpu),
			Memory:     uint64(p.memory),
			Restricted: p.restricted != 0,
		})
	}
	return processes, nil
}

// Signal sends the signal to the process, if sysctl says it started when it
// did.
func (darwin) Signal(p Process, number int) error {
	return signal(p, func(pid int) (uint64, bool) {
		var start C.ulonglong
		if C.process_start(C.int(pid), &start) != 0 {
			return 0, false
		}
		return uint64(start), true
	}, number)
}
