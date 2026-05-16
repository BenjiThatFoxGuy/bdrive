import { memo, useCallback, useState } from "react";
import type { UserSession } from "@/types";
import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import {
  Button,
  Input,
  Modal,
  Radio,
  RadioGroup,
  TextArea,
} from "@heroui/react";
import IcRoundContentCopy from "~icons/ic/round-content-copy";
import IcRoundRemoveCircleOutline from "~icons/ic/round-remove-circle-outline";
import clsx from "clsx";

import { Controller, useForm } from "react-hook-form";
import toast from "react-hot-toast";

import { copyDataToClipboard } from "@/utils/common";
import { scrollbarClasses } from "@/utils/classes";
import { $api } from "@/utils/api";

import type { components } from "@/lib/api";
import { NetworkError } from "@/utils/fetch-throw";
import SyncIcon from "~icons/material-symbols/sync";
import DeleteIcon from "~icons/material-symbols/delete";
import AddIcon from "~icons/material-symbols/add-circle";
import MaterialSymbolsSmartToy from "~icons/material-symbols/smart-toy";
import MaterialSymbolsTv from "~icons/material-symbols/tv";
import IcRoundSecurity from "~icons/ic/round-security";
import { ApiKeysCard } from "./api-keys-card";

const validateBots = (value?: string) => {
  if (value) {
    const regexPattern = /^\d{10}:[A-Za-z\d_-]{35}$/gm;
    return regexPattern.test(value) || "Invalid Token format";
  }
  return false;
};

const formatDate = (date: string) => {
  const d = new Date(date);
  return d.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
};

const Session = memo(
  ({ appName, location, createdAt, valid, sessionId, current }: UserSession) => {
    const deleteSession = $api.useMutation("delete", "/users/sessions/{id}", {
      onSettled: () => {
        queryClient.invalidateQueries({
          queryKey: $api.queryOptions("get", "/users/sessions").queryKey,
        });
      },
    });
    const queryClient = useQueryClient();

    return (
      <div className="bg-surface-secondary rounded-2xl p-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div
              className={clsx(
                "size-2 shrink-0 rounded-full",
                valid ? "bg-success" : "bg-danger",
              )}
            />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <p className="font-medium truncate">{appName || "Unknown"}</p>
                {current && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-accent-soft text-accent-soft-foreground font-semibold uppercase tracking-wider">
                    Current
                  </span>
                )}
              </div>
              <p className="text-sm text-muted truncate">
                Created {formatDate(createdAt)}
                {location && <> &middot; {location}</>}
              </p>
            </div>
          </div>
          {(!current || !valid) && (
            <Button
              isIconOnly
              variant="ghost"
              size="sm"
              className="shrink-0 text-muted hover:text-danger"
              onPress={() =>
                deleteSession.mutateAsync({ params: { path: { id: sessionId } } })
              }
            >
              <DeleteIcon className="size-4" />
            </Button>
          )}
        </div>
      </div>
    );
  },
);

const ChannelCreateDialog = ({ handleClose }: { handleClose: () => void }) => {
  const queryClient = useQueryClient();
  const createChannel = $api.useMutation("post", "/users/channels", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/users/channels"] });
      toast.success("Channel Added");
    },
  });

  const [channel, setChannel] = useState("");

  const onCreate = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      createChannel
        .mutateAsync({
          body: {
            channelName: channel,
          },
        })
        .then(() => handleClose());
    },
    [channel],
  );

  return (
    <>
      <Modal.Header className="flex flex-col gap-1">
        <Modal.Heading>Create Channel</Modal.Heading>
      </Modal.Header>
      <Modal.Body>
        <form id="add-channel" onSubmit={onCreate}>
          <Input
            className="border-large"
            placeholder="Channel Name"
            autoFocus
            value={channel}
            onChange={(e) => setChannel(e.target.value)}
          />
        </form>
      </Modal.Body>
      <Modal.Footer>
        <Button className="font-normal" variant="ghost" onPress={handleClose}>
          Close
        </Button>
        <Button
          type="submit"
          form="add-channel"
          className="font-normal"
          isDisabled={createChannel.isPending || !channel}
        >
          {createChannel.isPending ? "Creating" : "Create"}
        </Button>
      </Modal.Footer>
    </>
  );
};

const BotRemoveDialog = ({
  handleClose,
  onRemove,
}: {
  handleClose: () => void;
  onRemove: () => void;
}) => (
    <>
      <Modal.Header className="flex flex-col gap-1">
        <Modal.Heading>Remove All Bots</Modal.Heading>
      </Modal.Header>
      <Modal.Body>
        <p className="text-lg font-medium mt-2">
          Are you sure you want to remove all bots?
        </p>
        <p className="text-sm text-muted mt-1">
          This action cannot be undone.
        </p>
      </Modal.Body>
      <Modal.Footer>
        <Button className="font-normal" variant="ghost" onPress={handleClose}>
          Cancel
        </Button>
        <Button
          variant="secondary"
          className="font-normal"
          onPress={onRemove}
        >
          Remove All
        </Button>
      </Modal.Footer>
    </>
  );

const ChannelDeleteDialog = ({
  channelId,
  handleClose,
}: {
  channelId: number;
  handleClose: () => void;
}) => {
  const queryClient = useQueryClient();

  const deleteChannel = $api.useMutation("delete", "/users/channels/{id}", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/users/channels"] });
      toast.success("Channel Deleted");
    },
  });

  const onDelete = useCallback(() => {
    deleteChannel
      .mutateAsync({
        params: {
          path: {
            id: String(channelId),
          },
        },
      })
      .then(() => handleClose());
  }, [channelId]);
  return (
    <>
      <Modal.Header className="flex flex-col gap-1">
        <Modal.Heading>Delete Channel</Modal.Heading>
      </Modal.Header>
      <Modal.Body>
        <p className="text-lg font-medium mt-2">
          Are you sure you want to delete this channel?
        </p>
      </Modal.Body>
      <Modal.Footer>
        <Button className="font-normal" variant="ghost" onPress={handleClose}>
          No
        </Button>
        <Button
          variant="secondary"
          className="font-normal"
          onPress={onDelete}
        >
          Yes
        </Button>
      </Modal.Footer>
    </>
  );
};

interface ChannelOperationProps {
  open: boolean;
  handleClose: () => void;
  operation: "add" | "delete";
  channelId: number;
}

interface BotOperationProps {
  open: boolean;
  handleClose: () => void;
  onRemove: () => void;
}

const BotOperationModal = memo(
  ({ open, handleClose, onRemove }: BotOperationProps) => (
      <Modal.Backdrop isOpen={open} onOpenChange={(isOpen) => { if (!isOpen) {handleClose();} }}>
        <Modal.Container>
          <Modal.Dialog>
            <BotRemoveDialog handleClose={handleClose} onRemove={onRemove} />
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    ),
);

const ChannelOperationModal = memo(
  ({ open, handleClose, operation, channelId }: ChannelOperationProps) => {
    const renderOperation = () => {
      switch (operation) {
        case "add":
          return <ChannelCreateDialog handleClose={handleClose} />;
        case "delete":
          return (
            <ChannelDeleteDialog
              channelId={channelId}
              handleClose={handleClose}
            />
          );
        default:
          return null;
      }
    };
    return (
      <Modal.Backdrop isOpen={open} onOpenChange={(o) => { if (!o) {handleClose();} }}>
        <Modal.Container>
          <Modal.Dialog>
            {renderOperation()}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    );
  },
);

export const AccountTab = memo(() => {
  const { control, handleSubmit } = useForm<{ tokens: string }>({
    defaultValues: { tokens: "" },
  });

  const [{ data: userConfig }, { data: sessions }, { data: channelData }] =
    useSuspenseQueries({
      queries: [
        $api.queryOptions("get", "/users/config"),
        $api.queryOptions("get", "/users/sessions"),
        $api.queryOptions("get", "/users/channels"),
      ],
    });

  const removeBots = $api.useMutation("delete", "/users/bots", {
    onError: () => {
      toast.error("Failed to remove bots");
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/users/config"] });
      toast.success("All bots removed");
      setBotOpen(false);
    },
  });

  const handleRemoveBots = useCallback(() => {
    removeBots.mutate({});
  }, []);

  const syncChannels = $api.useMutation("patch", "/users/channels/sync", {
    onError: async (error) => {
      if (error instanceof NetworkError) {
        const errorData =
          (await error.data?.json()) as components["schemas"]["Error"];
        toast.error(
          `Sync failed: ${errorData.message.split(":").slice(-1)[0]!.trim()}`,
        );
      } else {
        toast.error("Sync failed: An unknown error occurred.");
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/users/channels"] });
      toast.success("Channels Synced");
    },
  });

  const queryClient = useQueryClient();

  const copyTokens = useCallback(() => {
    if (userConfig && userConfig.bots.length > 0) {
      copyDataToClipboard(userConfig.bots).then(() => {
        toast.success("Tokens Copied");
      });
    }
  }, [userConfig?.bots]);

  const botAddition = $api.useMutation("post", "/users/bots", {
    onError: async (error) => {
      if (error instanceof NetworkError) {
        const errorData =
          (await error.data?.json()) as components["schemas"]["Error"];
        toast.error(errorData.message.split(":").slice(-1)[0]!.trim());
      }
    },
    onSuccess: () => {
      toast.success("bots added");
      queryClient.invalidateQueries({ queryKey: ["get", "/users/config"] });
    },
  });

  const updateChannel = $api.useMutation("patch", "/users/channels", {
    onError: async (error) => {
      if (error instanceof NetworkError) {
        const errorData =
          (await error.data?.json()) as components["schemas"]["Error"];
        toast.error(
          `Failed to update default channel: ${errorData.message.split(":").slice(-1)[0]!.trim()}`,
        );
      } else {
        toast.error(
          "Failed to update default channel: An unknown error occurred.",
        );
      }
    },
    onSuccess: () => {
      toast.success("Default channel updated");
      queryClient.invalidateQueries({ queryKey: ["get", "/users/config"] });
    },
  });

  const onSubmit = useCallback(
    async ({ tokens }: { tokens: string }) => {
      botAddition.mutateAsync({
        body: {
          bots: tokens.trim().split("\n"),
        },
      });
    },
    [botAddition],
  );

  const handleSetDefaultChannel = useCallback(
    (channelId: number) => {
      const channel = channelData?.find((c) => c.channelId === channelId);
      if (channel) {
        updateChannel.mutate({
          body: {
            channelId: channel.channelId,
            channelName: channel.channelName,
          },
        });
      }
    },
    [channelData, updateChannel],
  );

  const [botOpen, setBotOpen] = useState(false);
  const [channelOpen, setChannelOpen] = useState(false);
  const [channelOperation, setChannelOperation] = useState<"add" | "delete">(
    "add",
  );
  const [channelID, setChannelID] = useState(0);

  return (
    <div
      className={clsx(
        "flex flex-col gap-6 p-4 w-full h-full overflow-y-auto",
        scrollbarClasses,
      )}
    >
      <div className="bg-background-secondary rounded-3xl p-6 border border-border/50">
        <div className="flex items-start gap-4">
          <div className="p-3 rounded-2xl bg-surface-secondary">
            <MaterialSymbolsSmartToy className="size-6 text-surface-secondary-foreground" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="text-xl font-semibold mb-1">Manage Bots</h3>
            <p className="text-sm text-muted">
              Add multiple bots to increase upload speeds.
            </p>
          </div>
        </div>
        <div className="mt-6 space-y-4">
          <form
            onSubmit={handleSubmit(onSubmit)}
            className="flex flex-col gap-4"
          >
            <Controller
              name="tokens"
              control={control}
              rules={{ required: true, validate: validateBots }}
              render={({ field }) => (
                <TextArea
                  {...field}
                  placeholder="Enter tokens (one per line)"
                  autoComplete="off"
                />
              )}
            />
            <Button
              type="submit"
              variant="primary"
              className="self-start px-6"
              isDisabled={botAddition.isPending}
            >
              {botAddition.isPending ? "Adding..." : "Add Bots"}
            </Button>
          </form>

          <div className="mt-8 pt-6 border-t border-border/30">
            <div className="flex justify-between items-center">
              <div>
                <p className="text-sm font-medium text-muted">
                  Active Bots
                </p>
                <p className="text-3xl font-bold mt-1 text-accent">
                  {userConfig?.bots.length || 0}
                </p>
              </div>
              <BotOperationModal
                open={botOpen}
                handleClose={() => setBotOpen(false)}
                onRemove={handleRemoveBots}
              />
              <div className="flex flex-col sm:flex-row gap-2">
                <Button
                  variant="secondary"
                  className="text-sm font-medium"
                  onPress={copyTokens}
                  isDisabled={userConfig?.bots.length === 0}
                >
                  <IcRoundContentCopy className="size-4" />
                  Copy All
                </Button>
                <Button
                  variant="secondary"
                  className="text-sm font-medium"
                  onPress={() => setBotOpen(true)}
                  isDisabled={userConfig?.bots.length === 0}
                >
                  <IcRoundRemoveCircleOutline className="size-4" />
                  Remove All
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="bg-background-secondary rounded-3xl p-6 border border-border/50 flex flex-col gap-6">
        <div className="flex flex-col sm:flex-row sm:justify-between sm:items-start gap-3">
          <div className="flex gap-4 items-start">
            <div className="p-3 rounded-2xl bg-surface-secondary text-surface-secondary-foreground">
              <MaterialSymbolsTv className="size-6" />
            </div>
            <div className="flex-1 min-w-0">
              <h3 className="text-xl font-semibold mb-1">Channels</h3>
              <p className="text-sm text-muted">
                Manage destination channels.
              </p>
            </div>
          </div>
          <ChannelOperationModal
            open={channelOpen}
            handleClose={() => setChannelOpen(false)}
            operation={channelOperation}
            channelId={channelID}
          />
          <div className="flex gap-2">
            <Button
              isIconOnly
              variant="ghost"
              onPress={() => {
                setChannelOperation("add");
                setChannelOpen(true);
              }}
            >
              <AddIcon className="size-6" />
            </Button>
            <Button
              isIconOnly
              variant="ghost"
              isDisabled={syncChannels.isPending}
              onPress={() => syncChannels.mutate({})}
            >
              <SyncIcon
                className={clsx(
                  "size-6",
                  syncChannels.isPending && "animate-spin",
                )}
              />
            </Button>
          </div>
        </div>

        <div>
          {channelData && channelData.length > 0 ? (
            <RadioGroup
              aria-label="Select Default Channel"
              value={userConfig.channelId?.toString() || ""}
              onChange={(value) => handleSetDefaultChannel(Number(value))}
            >
              {channelData.map((channel) => (
                <div
                  key={channel.channelId}
                  className="flex justify-between items-center p-4 rounded-2xl bg-surface hover:bg-surface-secondary transition-colors border border-transparent hover:border-border/30"
                >
                  <div className="flex-1 flex flex-col">
                    <Radio
                      value={channel.channelId!.toString()}
                      className="text-base font-semibold"
                    >
                      {channel.channelName}
                    </Radio>
                    <p className="text-sm text-muted ml-8 mt-0.5 font-mono">
                      ID: {channel.channelId}
                    </p>
                  </div>
                  <Button
                    isIconOnly
                    variant="ghost"

                    onPress={() => {
                      setChannelOperation("delete");
                      setChannelID(channel.channelId!);
                      setChannelOpen(true);
                    }}
                  >
                    <DeleteIcon className="size-5" />
                  </Button>
                </div>
              ))}
            </RadioGroup>
          ) : (
            <div className="flex flex-col items-center justify-center py-10 text-center bg-surface/50 rounded-2xl border-2 border-dashed border-border/30">
              <p className="text-sm text-muted max-w-xs">
                No channels found. Press the sync button to fetch your channels
                from Telegram.
              </p>
            </div>
          )}
        </div>
      </div>

      <ApiKeysCard />

      <div className="bg-surface rounded-3xl p-6 border border-border/50">
        <div className="flex items-start gap-4 mb-6">
          <div className="p-3 rounded-2xl bg-surface-secondary text-surface-secondary-foreground">
            <IcRoundSecurity className="size-6" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="text-xl font-semibold mb-1">Active Sessions</h3>
            <p className="text-sm text-muted">
              Devices currently logged in to your account.
            </p>
          </div>
        </div>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(280px,1fr))] gap-4">
          {sessions?.map((session) => (
            <Session key={session.sessionId} {...session} />
          ))}
        </div>
      </div>
    </div>
  );
});
