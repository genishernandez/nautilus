PROGRAM MainProgram
VAR_EXTERNAL
    StartPB    : BOOL;
    StopPB     : BOOL;
    RunCmd     : BOOL;
    HiLevelAlm : BOOL;
    LevelPct   : REAL;
    HiLevelSP  : REAL;
    Pump1      : Pump;
    SpareSP    : DINT;
END_VAR
VAR
    RunTmr   : TON;
    Starts   : CTU;
    LevelLog : ARRAY [0..3] OF REAL;
END_VAR
LD
  RUNG seal (* P-101 pump seal-in *)
    [ StartPB | RunCmd ] /StopPB ( RunCmd )

  RUNG alarm (* LAH-101 at or above the setpoint *)
    GE(LevelPct, HiLevelSP) ( HiLevelAlm )

  RUNG runtime (* P-101 run timer *)
    RunCmd RunTmr:TON(PT := T#30S)

  RUNG starts (* P-101 start count *)
    RunCmd Starts:CTU(PV := 1000, R := StopPB)

  RUNG speed (* P-101 running when commanded with a speed *)
    RunCmd GT(Pump1.Speed, 0.0) GE(LevelLog[0], 0.0) GE(SpareSP, 0) ( Pump1.Run )
END_LD
END_PROGRAM
