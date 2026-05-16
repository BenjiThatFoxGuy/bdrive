import { memo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Spinner } from "@heroui/react";
import toast from "react-hot-toast";

import { Button } from "@heroui/react";
import type { components } from "@/lib/api";
import { $api } from "@/utils/api";

type PeriodicJobSummary = components["schemas"]["PeriodicJobSummary"];

const kindLabel: Record<PeriodicJobSummary["kind"], string> = {
  "clean.old_events": "Clean Old Events",
  "clean.stale_uploads": "Clean Stale Uploads",
  "clean.pending_files": "Clean Pending Files",
  "refresh.folder_sizes": "Refresh Folder Sizes",
};

const formatDate = (value?: string) => (value ? new Date(value).toLocaleString() : "Never");

export const JobsTab = memo(() => {
  const queryClient = useQueryClient();
  const jobsQuery = $api.useQuery("get", "/periodic-jobs");
  const enableMutation = $api.useMutation("post", "/periodic-jobs/{id}/enable");
  const disableMutation = $api.useMutation("post", "/periodic-jobs/{id}/disable");
  const runMutation = $api.useMutation("post", "/periodic-jobs/{id}/run");

  const refreshJobs = () => {
    queryClient.invalidateQueries({ queryKey: ["get", "/periodic-jobs"] });
  };

  const toggleJob = (job: PeriodicJobSummary) => {
    const mutation = job.enabled ? disableMutation : enableMutation;
    mutation.mutate(
      { params: { path: { id: job.id } } },
      {
        onSuccess: () => {
          toast.success(job.enabled ? "Job disabled" : "Job enabled");
          refreshJobs();
        },
        onError: () => toast.error("Failed to update job"),
      },
    );
  };

  const runJob = (job: PeriodicJobSummary) => {
    runMutation.mutate(
      { params: { path: { id: job.id } } },
      {
        onSuccess: () => {
          toast.success("Job scheduled");
          refreshJobs();
        },
        onError: () => toast.error("Failed to schedule job"),
      },
    );
  };

  if (jobsQuery.isLoading) {
    return <Spinner className="m-auto flex" />;
  }

  const jobs = jobsQuery.data ?? [];

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-2">
      <div>
        <h2 className="text-xl font-semibold">Background Jobs</h2>
        <p className="text-sm text-muted">Cron-managed maintenance jobs for this instance.</p>
      </div>

      <div className="grid gap-3">
        {jobs.map((job) => (
          <section key={job.id} className="rounded-xl border border-border/30 bg-surface-secondary p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 space-y-1">
                <h3 className="font-medium">{job.name || kindLabel[job.kind]}</h3>
                <p className="text-sm text-muted">{kindLabel[job.kind]}</p>
                <p className="text-sm text-muted">Cron: {job.cronExpression}</p>
                <p className="text-sm text-muted">Next run: {formatDate(job.nextRunAt)}</p>
                <p className="text-sm text-muted">Last run: {formatDate(job.lastRunAt)}</p>
                {job.lastState && <p className="text-sm text-muted">Last state: {job.lastState}</p>}
                {job.lastError && <p className="text-sm text-danger">{job.lastError}</p>}
              </div>
              <div className="flex shrink-0 gap-2">
                <Button variant="outline" onPress={() => toggleJob(job)}>
                  {job.enabled ? "Disable" : "Enable"}
                </Button>
                <Button onPress={() => runJob(job)}>Run now</Button>
              </div>
            </div>
          </section>
        ))}
        {jobs.length === 0 && <p className="text-sm text-muted">No periodic jobs configured.</p>}
      </div>
    </div>
  );
});
