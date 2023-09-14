#define _GNU_SOURCE

#include <sched.h>
#include <stdio.h>
#include <unistd.h>
#include <stdlib.h>
#include <fcntl.h>
#include <string.h>

__attribute((constructor)) void enter(void) {
		uid_t uid = getuid();
    if (unshare(CLONE_NEWUSER) == -1) {
        perror("unshared call failed");
        exit(1);
    }
    // Create a string to hold the UID mapping
    char uid_mapping[50];  // Adjust the size as needed

    // Format the UID mapping string with the current UID
    snprintf(uid_mapping, sizeof(uid_mapping), "0 %d 1\n", (int)uid);

    int uid_map_fd = open("/proc/self/uid_map", O_WRONLY);
		if (uid_map_fd == -1) {
        perror("open uid_map failed");
        exit(1);
		}
    if (write(uid_map_fd, uid_mapping, strlen(uid_mapping)) == -1) {
        perror("write failed");
        exit(1);
    }
}
