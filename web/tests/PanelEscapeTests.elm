module PanelEscapeTests exposing (suite)

import App.PanelEscape as Escape


closed : Escape.Open
closed =
    { browserImport = False
    , upload = False
    , duplicates = False
    , similarity = False
    , moderation = False
    , recovery = False
    , drawers = False
    , grid = True
    }


suite : List ( String, Bool )
suite =
    [ ( "escape closes moderation before leaving the grid"
      , Escape.next { closed | moderation = True } == Escape.CloseModeration
      )
    , ( "escape closes recovery before a covering drawer"
      , Escape.next { closed | recovery = True, drawers = True } == Escape.CloseRecovery
      )
    , ( "escape closes the upload queue before moderation"
      , Escape.next { closed | upload = True, moderation = True } == Escape.CloseUpload
      )
    , ( "escape leaves the grid when no workflow panel is open"
      , Escape.next closed == Escape.Idle
      )
    , ( "escape exits loupe only after workflow panels are closed"
      , Escape.next { closed | grid = False } == Escape.ExitMode
            && Escape.next { closed | grid = False, recovery = True } == Escape.CloseRecovery
      )
    ]
