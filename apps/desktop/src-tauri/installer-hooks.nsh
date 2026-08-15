!macro NSIS_HOOK_POSTINSTALL
  CreateShortcut "$DESKTOP\Aggregation Hub.lnk" "$INSTDIR\Aggregation Hub.exe"
!macroend

!macro NSIS_HOOK_POSTUNINSTALL
  Delete "$DESKTOP\Aggregation Hub.lnk"
!macroend