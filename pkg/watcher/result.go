/*
 * Copyright (c) 2019 vChain, Inc. All Rights Reserved.
 * This software is released under GPL3.
 * The full license information can be found under:
 * https://www.gnu.org/licenses/gpl-3.0.en.html
 *
 */

package watcher

import (
	"encoding/json"
	"fmt"
	"net/http"

	vcn "github.com/vchain-us/vcn/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

// ContainerInfo represents the reference to a running container
type ContainerInfo struct {
	Namespace   string `json:"namespace"`
	Pod         string `json:"pod"`
	Container   string `json:"container"`
	ContainerID string `json:"containerID"`
	Image       string `json:"image"`
	ImageID     string `json:"imageID"`
}

// Result represents a watcher inspection of a container
type Result struct {
	Hash         string                      `json:"hash"`
	Containers   []ContainerInfo             `json:"containers"`
	Verification *vcn.BlockchainVerification `json:"verification,omitempty"`
	Errors       []error                     `json:"errors,omitempty"`
}

func (w *watchdog) commit() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// clean unseen
	for hash, seen := range w.seen {
		if seen {
			w.seen[hash] = false
		} else {
			delete(w.res, hash)
			delete(w.seen, hash)
		}
	}

	// commit index
	w.idx = make([]string, len(w.tmp))
	copy(w.idx, w.tmp)
	w.tmp = []string{}
}

func (w *watchdog) upsert(pod corev1.Pod, status corev1.ContainerStatus, hash string, v *vcn.BlockchainVerification, errs []error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.seen[hash] {
		w.tmp = append(w.tmp, hash)
	}

	// check if already present, otherwise create a new Result
	r, found := w.res[hash]
	if !found {
		r = Result{
			Hash:         hash,
			Containers:   []ContainerInfo{},
			Verification: v,
			Errors:       errs,
		}
	}

	// update verification
	r.Verification = v

	// update containers info
	found = false
	for _, c := range r.Containers {
		if c.ContainerID == status.ContainerID {
			found = true
		}
	}
	if !found {
		r.Containers = append(r.Containers, ContainerInfo{
			Namespace:   pod.Namespace,
			Pod:         pod.Name,
			Container:   status.Name,
			ContainerID: status.ContainerID,
			Image:       status.Image,
			ImageID:     status.ImageID,
		})
	}

	// mark hash as seen and save the result
	w.seen[hash] = true
	w.res[hash] = r
}

// ResultsHandler returns an http.Handler to expose detailed verification results.
func (w *watchdog) ResultsHandler() http.Handler {
	ww := w
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww.mu.RLock()
		defer ww.mu.RUnlock()

		res := make([]Result, len(ww.idx))
		for i, hash := range ww.idx {
			res[i] = ww.res[hash]
		}

		b, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, err.Error())
			return
		}

		headers := w.Header()
		headers.Set("Access-Control-Allow-Origin", "*")
		headers.Set("Content-Type", "application/json")
		w.Write(b)
	})
}
