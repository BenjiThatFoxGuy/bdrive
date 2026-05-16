import { memo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Spinner } from "@heroui/react";
import clsx from "clsx";
import toast from "react-hot-toast";

import { Button } from "@heroui/react";
import type { components } from "@/lib/api";
import { $api } from "@/utils/api";
import { scrollbarClasses } from "@/utils/classes";
import MaterialSymbolsScheduleRounded from "~icons/material-symbols/schedule-rounded";

type PeriodicJobSummary = components["schemas"]["PeriodicJobSummary"];

const kindLabel: Record<PeriodicJobSummary["kind"], string> = {
  "clean.old_events": "Clean Old Events",
  "clean.pending_files": "Clean Pending Files",
  "clean.stale_uploads": "Clean Stale Uploads",
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
        onError: () => toast.error("Failed to update job"),
        onSuccess: () => {
          toast.success(job.enabled ? "Job disabled" : "Job enabled");
          refreshJobs();
        },
      },
    );
  };

  const runJob = (job: PeriodicJobSummary) => {
    runMutation.mutate(
      { params: { path: { id: job.id } } },
      {
        onError: () => toast.error("Failed to schedule job"),
        onSuccess: () => {
          toast.success("Job scheduled");
          refreshJobs();
        },
      },
    );
  };

  if (jobsQuery.isLoading) {
    return <Spinner className="m-auto flex" />;
  }

  const jobs = jobsQuery.data ?? [];

  return (
    <div className={clsx("flex flex-col gap-6 p-4 h-full overflow-y-auto", scrollbarClasses)}>
      <div className="bg-surface rounded-3xl p-6 border border-border/50">
        <div className="flex items-start gap-4 mb-6">
          <div className="p-3 rounded-2xl bg-surface-secondary text-surface-secondary-foreground">
            <MaterialSymbolsScheduleRounded className="size-6" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="text-xl font-semibold mb-1">Background Jobs</h3>
            <p className="text-sm text-muted">
              Cron-managed maintenance jobs for this instance.
            </p>
          </div>
        </div>

        {jobs.length > 0 ? (
          <div className="space-y-2">
            {jobs.map((job) => (
              <div
                key={job.id}
                className="bg-surface-secondary rounded-2xl p-4 border border-border/30 transition-colors hover:border-border/60"
              >
                <div className="flex items-center justify-between gap-4">
                  <div className="flex items-center gap-3 min-w-0 flex-1">
                    <div
                      className={clsx(
                        "size-2 shrink-0 rounded-full",
                        job.enabled ? "bg-success" : "bg-muted",
                      )}
                    />
                    <div className="min-w-0">
                      <p className="font-medium">{job.name || kindLabel[job.kind]}</p>
                      <p className="text-sm text-muted truncate">
                        {kindLabel[job.kind]}
                        {job.lastState && <> &middot; {job.lastState}</>}
                        {job.nextRunAt && <> &middot; Next {formatDate(job.nextRunAt)}</>}
                        {job.lastRunAt && <> &middot; Last {formatDate(job.lastRunAt)}</>}
                      </p>
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      variant={job.enabled ? "ghost" : "secondary"}
                      size="sm"
                      onPress={() => toggleJob(job)}
                    >
                      {job.enabled ? "Disable" : "Enable"}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onPress={() => runJob(job)}
                    >
                      Run
                    </Button>
                  </div>
                </div>
                {job.lastError && (
                  <p className="mt-2 text-sm text-danger truncate">{job.lastError}</p>
                )}
              </div>
            ))}
          </div>
        ) : (
          <div className="rounded-2xl border-2 border-dashed border-border/30 bg-surface/50 p-8 text-center">
            <p className="text-sm text-muted mb-1">No jobs configured</p>
            <p className="text-xs text-muted/60">Periodic maintenance jobs will appear here.</p>
          </div>
        )}
      </div>
    </div>
  );
});
