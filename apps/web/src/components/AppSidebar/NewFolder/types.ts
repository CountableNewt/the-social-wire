import type { ReactNode } from "react";

export type CreateFolderCreatedPayload = { uri: string };

export interface CreateFolderFormFieldsProps {
  onOpenChange: (open: boolean) => void;
  onCreated?: (payload: CreateFolderCreatedPayload) => void | Promise<void>;
  description?: ReactNode;
  dialogTitle?: string;
  submitLabel?: string;
  pendingSubmitLabel?: string;
}

export interface ControlledCreateFolderDialogProps
  extends CreateFolderFormFieldsProps {
  open: boolean;
}
