module SearchTests exposing (allPassed, suite)

import Feature.Search as Search
import Http


ctx : Search.Context
ctx =
    Search.context ""


startModel : String -> Search.Model
startModel query =
    Search.start ctx query Search.init |> Tuple.first


settle : Search.Msg -> Search.Model -> ( Search.Model, List Search.OutMsg )
settle msg model =
    let
        ( settled, _, outs ) =
            Search.update ctx msg model
    in
    ( settled, outs )


paginate : Search.Model -> Search.Model
paginate model =
    Search.requestMore ctx model |> Tuple.first


started : Search.Model
started =
    startModel "cats"


caseStartSubmits : Bool
caseStartSubmits =
    Search.query started == "cats" && Search.state started == Search.Searching && Search.nextCursor started == Nothing


caseStartBumpsGeneration : Bool
caseStartBumpsGeneration =
    Search.requestId started == Search.requestId Search.init + 1


caseStaleInitialIgnored : Bool
caseStaleInitialIgnored =
    let
        response =
            { posts = [], nextCursor = Just "c1" }

        ( settled, outs ) =
            settle (Search.Completed 999 Search.InitialRequest (Ok response)) started
    in
    outs == [] && settled == started


caseStaleMoreIgnored : Bool
caseStaleMoreIgnored =
    let
        ( settled, outs ) =
            settle (Search.Completed 999 Search.MoreRequest (Err Http.Timeout)) started
    in
    outs == [] && settled == started


caseInitialArrives : Bool
caseInitialArrives =
    let
        response =
            { posts = [], nextCursor = Just "c1" }

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok response)) started
    in
    outs == [ Search.InitialArrived response ] && Search.state settled == Search.Ready && Search.nextCursor settled == Just "c1"


caseMoreArrives : Bool
caseMoreArrives =
    let
        first =
            { posts = [], nextCursor = Just "c1" }

        second =
            { posts = [], nextCursor = Nothing }

        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok first)) started

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId ready) Search.MoreRequest (Ok second)) (paginate ready)
    in
    outs == [ Search.MoreArrived second ] && Search.state settled == Search.Ready && Search.nextCursor settled == Nothing


caseFailureReported : Bool
caseFailureReported =
    let
        ( settled, outs ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Err Http.Timeout)) started
    in
    outs == [ Search.SearchFailed Http.Timeout ] && Search.state settled == Search.Failed "The search timed out."


caseErrorTextRetainsBadUrlAndStatus : Bool
caseErrorTextRetainsBadUrlAndStatus =
    Search.errorToString (Http.BadUrl "https://bad.invalid") == "Invalid API URL: https://bad.invalid"
        && Search.errorToString (Http.BadStatus 409) == "Search failed (HTTP 409)."


caseRequestMoreWithoutCursorNoop : Bool
caseRequestMoreWithoutCursorNoop =
    paginate started == started


caseRequestMoreEntersLoading : Bool
caseRequestMoreEntersLoading =
    let
        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok { posts = [], nextCursor = Just "c1" })) started

        paged =
            paginate ready
    in
    Search.state paged == Search.LoadingMore && Search.nextCursor paged == Just "c1"


caseResetClears : Bool
caseResetClears =
    let
        cleared =
            Search.reset started
    in
    Search.query cleared == "" && Search.state cleared == Search.Idle && Search.nextCursor cleared == Nothing


caseRestartResubmits : Bool
caseRestartResubmits =
    let
        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok { posts = [], nextCursor = Nothing })) started

        restarted =
            Search.restart ctx ready |> Tuple.first
    in
    Search.state restarted == Search.Searching && Search.query restarted == "cats" && Search.nextCursor restarted == Nothing


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "start submits the query as searching", caseStartSubmits )
    , ( "start bumps the request generation", caseStartBumpsGeneration )
    , ( "stale initial completion ignored", caseStaleInitialIgnored )
    , ( "stale follow-up completion ignored", caseStaleMoreIgnored )
    , ( "initial page arrives ready with cursor", caseInitialArrives )
    , ( "next page arrives ready with cursor", caseMoreArrives )
    , ( "failure reports and keeps the message", caseFailureReported )
    , ( "search error text preserves URLs and status codes", caseErrorTextRetainsBadUrlAndStatus )
    , ( "pagination without a cursor is a no-op", caseRequestMoreWithoutCursorNoop )
    , ( "pagination with a cursor loads more", caseRequestMoreEntersLoading )
    , ( "reset clears back to idle", caseResetClears )
    , ( "restart resubmits the current query", caseRestartResubmits )
    ]
