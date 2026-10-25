package stats

/*
#include <stdlib.h>
#include <string.h>
#include <mach/mach.h>
#include <mach/mach_host.h>
#include <mach/processor_info.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/sysctl.h>
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
*/
import "C"

import (
	"errors"
	"unsafe"
)

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
