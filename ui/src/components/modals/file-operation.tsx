import { memo, useCallback, useEffect, useState } from "react";
import { FbActions } from "file-browser";
import {
  Button,
  Input,
  Modal,
  Separator,
  Switch,
} from "@heroui/react";
import { useShallow } from "zustand/react/shallow";

import { useModalStore } from "@/utils/stores";
import { Controller, useForm } from "react-hook-form";
import { CustomActions } from "@/hooks/use-file-action";
import { CopyButton } from "@/components/copy-button";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import IcRoundClose from "~icons/ic/round-close";
import { getNextDate } from "@/utils/common";
import ShowPasswordIcon from "~icons/mdi/eye-outline";
import HidePasswordIcon from "~icons/mdi/eye-off-outline";
import MdiProtectedOutline from "~icons/mdi/protected-outline";
import { $api } from "@/utils/api";
import { useSearch } from "@tanstack/react-router";

type FileModalProps = {
  queryKey: any;
};

interface RenameDialogProps {
  queryKey: any;
  handleClose: () => void;
}

const RenameDialog = memo(({ queryKey, handleClose }: RenameDialogProps) => {
  const queryClient = useQueryClient();
  const updateFiles = $api.useMutation("patch", "/files/{id}", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey });
    },
  });
  const { currentFile, actions } = useModalStore(
    useShallow((state) => ({
      currentFile: state.currentFile,
      actions: state.actions,
    })),
  );

  const onRename = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      updateFiles
        .mutateAsync({
          params: {
            path: {
              id: currentFile.id,
            },
          },
          body: {
            name: currentFile?.name,
          },
        })
        .then(handleClose);
    },
    [currentFile.name, currentFile.id],
  );

  return (
    <>
      <Modal.Header className="flex flex-col gap-1">
        <Modal.Heading>Rename</Modal.Heading>
      </Modal.Header>
      <Modal.Body>
        <form id="rename-form" onSubmit={onRename}>
          <Input
            className="border-large"
            variant="secondary"
            autoFocus
            value={currentFile.name}
            onChange={(e) => actions.setCurrentFile({ ...currentFile, name: e.target.value })}
          />
        </form>
      </Modal.Body>
      <Modal.Footer>
        <Button className="font-normal" variant="ghost" onPress={handleClose}>
          Close
        </Button>
        <Button
          type="submit"
          className="font-normal"
          variant="secondary"
          form="rename-form"
          isDisabled={updateFiles.isPending || !currentFile.name}
        >
          Rename
        </Button>
      </Modal.Footer>
    </>
  );
});

interface FolderCreateDialogProps {
  queryKey: any;
  handleClose: () => void;
}

const FolderCreateDialog = memo(({ queryKey, handleClose }: FolderCreateDialogProps) => {
  const queryClient = useQueryClient();

  const { path } = useSearch({ from: "/_authed/$view" });

  const createFolder = $api.useMutation("post", "/files", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey });
    },
  });
  const { currentFile, actions } = useModalStore(
    useShallow((state) => ({
      currentFile: state.currentFile,
      actions: state.actions,
    })),
  );

  const onCreate = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      createFolder
        .mutateAsync({
          body: {
            name: currentFile.name,
            type: "folder",
            path: path ? path : "/",
          },
        })
        .then(() => handleClose());
    },
    [currentFile.name],
  );

  return (
    <>
      <Modal.Header className="flex flex-col gap-1">
        <Modal.Heading>Create Folder</Modal.Heading>
      </Modal.Header>
      <Modal.Body>
        <form id="create-folder-form" onSubmit={onCreate}>
          <Input
            className="border-large"
            variant="secondary"
            placeholder="Folder Name or Path"
            autoFocus
            value={currentFile?.name}
            onChange={(e) => actions.setCurrentFile({ ...currentFile, name: e.target.value })}
          />
        </form>
      </Modal.Body>
      <Modal.Footer>
        <Button className="font-normal" variant="ghost" onPress={handleClose}>
          Close
        </Button>
        <Button
          type="submit"
          form="create-folder-form"
          className="font-normal"
          variant="secondary"
          isDisabled={createFolder.isPending || !currentFile.name}
        >
          {createFolder.isPending ? "Creating" : "Create"}
        </Button>
      </Modal.Footer>
    </>
  );
});

interface DeleteDialogProps {
  queryKey: any;
  handleClose: () => void;
}

const DeleteDialog = memo(({ handleClose, queryKey }: DeleteDialogProps) => {
  const queryClient = useQueryClient();

  const deleteFiles = $api.useMutation("post", "/files/delete", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey });
    },
  });

  const selectedFiles = useModalStore((state) => state.selectedFiles) as string[];

  const onDelete = useCallback(() => {
    deleteFiles.mutateAsync({ body: { ids: selectedFiles } });
    handleClose();
  }, [selectedFiles]);

  return (
    <>
      <Modal.Header className="flex flex-col gap-1">
        <Modal.Heading>Delete Files</Modal.Heading>
      </Modal.Header>
      <Modal.Body>
        <h1 className="text-large font-medium mt-2">
          {`Are you sure to delete ${selectedFiles.length} file${
            selectedFiles.length > 1 ? "s" : ""
          } ?`}
        </h1>
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
});

interface ShareFileDialogProps {
  handleClose: () => void;
}

const defaultShareOptions = {
  expirationDate: "",
  password: "",
};

const ShareFileDialog = memo(({ handleClose }: ShareFileDialogProps) => {
  const file = useModalStore((state) => state.currentFile);

  const queryClient = useQueryClient();

  const { control, handleSubmit } = useForm({
    defaultValues: defaultShareOptions,
  });

  const shareQueryOptions = $api.queryOptions("get", "/files/{id}/shares", {
    params: {
      path: {
        id: file.id,
      },
    },
  });

  const { data, isLoading } = useQuery(shareQueryOptions);

  const createShare = $api.useMutation("post", "/files/{id}/shares", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: shareQueryOptions.queryKey });
      queryClient.invalidateQueries({ queryKey: ["Files_list", "shared"] });
    },
  });

  const deleteShare = $api.useMutation("delete", "/files/{id}/shares/{shareId}", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["Files_list", "shared"] });
    },
  });

  const [sharingOn, setSharingOn] = useState(false);

  const [shareLink, setShareLink] = useState("");

  const [showPassword, setShowPassword] = useState(false);

  const onShareChange = useCallback(() => {
    setSharingOn((prev) => {
      if (!prev) {
        handleSubmit((data) => {
          const payload = {} as { password?: string; expiresAt?: string };
          if (data.expirationDate) {
            payload.expiresAt = `${data.expirationDate}${new Date().toISOString().slice(10)}`;
          }
          if (data.password) {
            payload.password = data.password;
          }
          createShare.mutateAsync({
            params: {
              path: {
                id: file.id,
              },
            },
            body: payload,
          });
        })();
      }
      if (prev) {
        const shareId = data?.[0]?.id;
        if (!shareId) {
          setShareLink("");
          return !prev;
        }
        deleteShare.mutateAsync({
          params: {
            path: {
              id: file.id,
              shareId,
            },
          },
        });
        setShareLink("");
      }
      return !prev;
    });
  }, []);

  useEffect(() => {
    if (data && data.length > 0) {
      setSharingOn(true);
      setShareLink(`${window.location.origin}/share/${data[0].id}`);
    }
  }, [data]);

  return (
    <>
      <Modal.Header className="flex items-center justify-between ">
        <Modal.Heading>Share Files</Modal.Heading>
        <Button size="sm" variant="ghost" isIconOnly onPress={handleClose}>
          <IcRoundClose />
        </Button>
      </Modal.Header>
      <Modal.Body>
        <form className="grid grid-cols-6 gap-8 p-2 w-full overflow-y-auto">
          <div className="col-span-6 xs:col-span-3">
            <p className="text-lg font-medium">Set expiration date</p>
            <p className="text-sm font-normal text-muted">Link expiration date</p>
          </div>
          <Controller
            name="expirationDate"
            control={control}
            render={({ field }) => (
              <Input
                className="col-span-6 xs:col-span-3"
                variant="secondary"
                type="date"
                min={getNextDate()}
                {...field}
              />
            )}
          />
          <div className="col-span-6 xs:col-span-3">
            <p className="text-lg font-medium">Set link password</p>
            <p className="text-sm font-normal text-muted">Public link password</p>
          </div>
          <Controller
            name="password"
            control={control}
            render={({ field }) => (
              <div className="col-span-6 xs:col-span-3 relative">
                <Input
                  className="col-span-6 xs:col-span-3"
                  variant="secondary"
                  autoComplete="off"
                  type={showPassword ? "text" : "password"}
                  {...field}
                />
                <Button
                  isIconOnly
                  className="size-8 min-w-8 absolute right-2 top-1/2 -translate-y-1/2 z-10"
                  variant="ghost"
                  onPress={() => setShowPassword((prev) => !prev)}
                >
                  {showPassword ? <HidePasswordIcon /> : <ShowPasswordIcon />}
                </Button>
              </div>
            )}
          />
        </form>
        <Separator />
        <div className="flex justify-between">
          <h1 className="text-large font-medium mt-2">Sharing {sharingOn ? "On" : "Off"}</h1>
          <div className="flex items-center gap-3">
            {data?.[0]?.protected && <MdiProtectedOutline className="text-accent" />}

            <Switch isSelected={sharingOn} onChange={onShareChange} size="md" aria-label="Toggle sharing">
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
            </Switch>
          </div>
        </div>
      </Modal.Body>
      <Modal.Footer>
        <Input
          disabled={isLoading || !data || data.length === 0}
          fullWidth
          variant="secondary"
          readOnly
          value={shareLink}
        />
        <CopyButton value={shareLink} isDisabled={isLoading || !data || data.length === 0} />
      </Modal.Footer>
    </>
  );
});

export const FileOperationModal = memo(({ queryKey }: FileModalProps) => {
  const { open, operation, actions } = useModalStore(
    useShallow((state) => ({
      open: state.open,
      operation: state.operation,
      actions: state.actions,
    })),
  );

  const handleClose = useCallback(
    () =>
      actions.set({
        open: false,
      }),
    [],
  );

  const renderOperation = () => {
    switch (operation) {
      case FbActions.RenameFile.id:
        return <RenameDialog queryKey={queryKey} handleClose={handleClose} />;
      case FbActions.CreateFolder.id:
        return <FolderCreateDialog queryKey={queryKey} handleClose={handleClose} />;
      case FbActions.DeleteFiles.id:
        return <DeleteDialog queryKey={queryKey} handleClose={handleClose} />;
      case CustomActions.ShareFiles.id:
        return <ShareFileDialog handleClose={handleClose} />;
      default:
        return null;
    }
  };

  return (
    <Modal.Backdrop isOpen={open} onOpenChange={(isOpen) => { if (!isOpen) handleClose(); }}>
      <Modal.Container>
        <Modal.Dialog>
          {renderOperation()}
        </Modal.Dialog>
      </Modal.Container>
    </Modal.Backdrop>
  );
});
