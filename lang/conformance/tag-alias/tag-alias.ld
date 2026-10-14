PROGRAM AliasLd
(* The same seal-in as a rung, on aliased tags. *)
VAR_EXTERNAL
    StartPB : BOOL;
    StopPB  : BOOL;
    ld_Run  : BOOL;
END_VAR
LD
  RUNG seal
    [ StartPB | ld_Run ] /StopPB ( ld_Run )
END_LD
END_PROGRAM
