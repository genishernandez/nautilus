TYPE
  LineStatus : STRUCT
    Mode  : DINT;
    Alarm : BOOL;
  END_STRUCT;
  Pump : STRUCT
    Run    : BOOL;
    Speed  : REAL;
    Status : LineStatus;
  END_STRUCT;
END_TYPE
