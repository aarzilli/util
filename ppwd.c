#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/param.h>

int main(int argc, char *argv[]) {
	char *home = getenv("HOME");
	int reqlen;
	int always_home = 0;
	char currentdir[MAXPATHLEN];
	char compr[MAXPATHLEN];
	char *lastslash;
	int i, j;

	if (argc <= 1) {
		reqlen = MAXPATHLEN+2;
	} else {
		reqlen = atoi(argv[1]);
	}

	if (reqlen < 0) {
		reqlen = -reqlen;
		always_home = 1;
	}

	getwd(currentdir);

	if (!always_home && (strlen(currentdir) < reqlen)) {
		puts(currentdir);
		return 0;
	}

	i = j = 0;
	if (home != NULL) {
		for(j = 0; j < strlen(home); ++j) {
			if (home[j] != currentdir[j]) { 
				j = 0;
				break;
			}
		}
		if (j != 0)
			compr[i++] = '~';
	}

	lastslash = strrchr(currentdir, '/');

	while((i + strlen(currentdir+j)) > reqlen) {
		if (currentdir+j == lastslash) break;
		if (currentdir[j] == '\0') break;
		if (currentdir[j] == '/') {
			compr[i++] = '/';
			++j;
			continue;
		} else {
			compr[i++] = currentdir[j++];
			for(; currentdir[j] != '\0' && currentdir[j] != '/'; ++j)
				; // skips rest of directory
		}
	}

	strcpy(compr+i, currentdir+j);
	puts(compr);
	return 0;
}
