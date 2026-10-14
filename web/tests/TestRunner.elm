port module TestRunner exposing (main)

import AccessTests
import ApiErrorTests
import CollectionBoundedTests
import DailyUseTests
import CollectionLoadMoreTests
import CollectionRefreshTests
import Json.Encode as Encode
import Platform
import CollectionsTests
import RevisionTests
import RatingsTests
import ReactionsTests
import SearchTests
import TagsTests
import TagManagementTests
import QueueTests
import UploadQueueTests
import WorkspaceControlTests
import ImportTests
import MetadataTests
import BulkMetadataTests
import DuplicatesTests


port run : (() -> msg) -> Sub msg


port report : Encode.Value -> Cmd msg


type Msg
    = Run


allTests : List ( String, Bool )
allTests =
    RevisionTests.suite ++ UploadQueueTests.suite ++ QueueTests.suite ++ AccessTests.suite ++ ApiErrorTests.suite ++ CollectionRefreshTests.suite ++ CollectionLoadMoreTests.suite ++ CollectionBoundedTests.suite ++ DailyUseTests.suite ++ WorkspaceControlTests.suite ++ SearchTests.suite ++ TagsTests.suite ++ RatingsTests.suite ++ ReactionsTests.suite ++ CollectionsTests.suite ++ ImportTests.suite ++ MetadataTests.suite ++ BulkMetadataTests.suite ++ TagManagementTests.suite ++ DuplicatesTests.suite


encodeTest : ( String, Bool ) -> Encode.Value
encodeTest ( name, passed ) =
    Encode.object
        [ ( "name", Encode.string name )
        , ( "passed", Encode.bool passed )
        ]


main : Program () () Msg
main =
    Platform.worker
        { init = \() -> ( (), Cmd.none )
        , update =
            \_ model ->
                ( model
                , report (Encode.list encodeTest allTests)
                )
        , subscriptions = \_ -> run (\_ -> Run)
        }
